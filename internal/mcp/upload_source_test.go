package mcp

import (
	"context"
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestUploadSourceAvailableByTransport(t *testing.T) {
	cases := []struct {
		name string
		mode FileSourceMode
		// matrix of transport -> expected availability
		stdio, http, openai bool
	}{
		{"path", SourcePath, true, false, false},
		{"mint", SourceMint, false, true, false},
		{"url", SourceURL, false, false, true},
		{"data", SourceData, false, false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := UploadSource{Mode: tc.mode}
			require.Equal(t, tc.stdio, s.Available(TransportStdio), "stdio")
			require.Equal(t, tc.http, s.Available(TransportHTTP), "http")
			require.Equal(t, tc.openai, s.Available(TransportOpenAI), "openai")
		})
	}
}

func TestUploadSourceValidate(t *testing.T) {
	t.Run("rejects unknown mode", func(t *testing.T) {
		err := (UploadSource{Mode: "nope"}).Validate(TransportStdio)
		require.Error(t, err)
	})
	t.Run("path requires value and is stdio-only", func(t *testing.T) {
		require.Error(t, (UploadSource{Mode: SourcePath}).Validate(TransportStdio), "empty path")
		err := (UploadSource{Mode: SourcePath, Path: "/x"}).Validate(TransportStdio)
		require.NoError(t, err)
		require.Error(t, (UploadSource{Mode: SourcePath, Path: "/x"}).Validate(TransportHTTP))
	})
	t.Run("mint is http-only", func(t *testing.T) {
		s := UploadSource{Mode: SourceMint}
		require.NoError(t, s.Validate(TransportHTTP))
		require.Error(t, s.Validate(TransportStdio), "mint invalid in stdio")
		require.Error(t, s.Validate(TransportOpenAI), "mint invalid on openai tunnel")
	})
	t.Run("url requires value and is openai-only", func(t *testing.T) {
		require.Error(t, (UploadSource{Mode: SourceURL}).Validate(TransportOpenAI), "empty url")
		require.NoError(t, (UploadSource{Mode: SourceURL, URL: "https://x/y"}).Validate(TransportOpenAI))
		require.Error(t, (UploadSource{Mode: SourceURL, URL: "https://x/y"}).Validate(TransportHTTP))
	})
	t.Run("data requires value and is openai-only", func(t *testing.T) {
		require.Error(t, (UploadSource{Mode: SourceData}).Validate(TransportOpenAI), "empty data")
		require.NoError(t, (UploadSource{Mode: SourceData, Data: "data:;base64,AAAA"}).Validate(TransportOpenAI))
		require.Error(t, (UploadSource{Mode: SourceData, Data: "data:;base64,AAAA"}).Validate(TransportStdio))
	})
}

func TestRelayURLName(t *testing.T) {
	require.Equal(t, "report.pdf", relayURLName("https://host/path/report.pdf"))
	require.Equal(t, "", relayURLName("https://host/"))
	require.Equal(t, "", relayURLName("not a url"))
}

func TestSourceResolverMintURL(t *testing.T) {
	cu := NewHTTPUpload(nil, 0)
	defer cu.Stop(context.Background())
	res := &SourceResolver{Transport: TransportHTTP, HTTPUpload: cu}

	url, err := res.MintURL(UploadSource{Mode: SourceMint}, "m.txt", time.Minute)
	require.NoError(t, err)
	require.NotEmpty(t, url, "minted URL non-empty")

	// Mint not valid off the HTTP transport.
	res2 := &SourceResolver{Transport: TransportStdio, HTTPUpload: cu}
	_, err = res2.MintURL(UploadSource{Mode: SourceMint}, "m.txt", time.Minute)
	require.Error(t, err, "mint invalid in stdio")

	// Missing coordinator.
	res3 := &SourceResolver{Transport: TransportHTTP}
	_, err = res3.MintURL(UploadSource{Mode: SourceMint}, "m.txt", time.Minute)
	require.Error(t, err, "missing coordinator")
}

func TestSourceResolverOpenBytesData(t *testing.T) {
	res := &SourceResolver{Transport: TransportOpenAI, RelayMaxBytes: 1 << 20}
	payload := []byte("hello vault")
	uri := "data:;name=note.txt;size=" + strconv.Itoa(len(payload)) + ";base64," + base64.StdEncoding.EncodeToString(payload)

	body, size, name, err := res.OpenBytes(context.Background(), UploadSource{Mode: SourceData, Data: uri})
	require.NoError(t, err)
	defer body.Close()
	require.Equal(t, int64(len(payload)), size)
	require.Equal(t, "note.txt", name)
	got, _ := io.ReadAll(body)
	require.Equal(t, "hello vault", string(got))
}

func TestSourceResolverOpenBytesURL(t *testing.T) {
	// Serve a tiny payload on a loopback TLS listener the relay can fetch.
	payload := []byte("url bytes")
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(payload)
	}))
	defer srv.Close()

	res := &SourceResolver{
		Transport:         TransportOpenAI,
		RelayMaxBytes:     1 << 20,
		RelayAllowedHosts: []string{"127.0.0.1"},
		RelayHTTPClient:   srv.Client(),
	}
	body, size, name, err := res.OpenBytes(context.Background(), UploadSource{Mode: SourceURL, URL: srv.URL + "/dir/file.txt"})
	require.NoError(t, err)
	defer body.Close()
	require.Equal(t, "file.txt", name)
	got, _ := io.ReadAll(body)
	require.Equal(t, "url bytes", string(got))
	require.Equal(t, int64(len(payload)), size)
}

func TestSourceResolverRejectsBadModeAndTransport(t *testing.T) {
	res := &SourceResolver{Transport: TransportHTTP}
	// path cannot OpenBytes (not a stream mode).
	_, _, _, err := res.OpenBytes(context.Background(), UploadSource{Mode: SourcePath, Path: "/x"})
	require.Error(t, err)
	// data not valid on the HTTP transport.
	_, _, _, err = res.OpenBytes(context.Background(), UploadSource{Mode: SourceData, Data: "data:;base64,AAAA"})
	require.Error(t, err)
}
