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

	"go.lumeweb.com/pinner-cli/internal/mcp/core/transfer"
)

func TestUploadSourceAvailableByTransport(t *testing.T) {
	cases := []struct {
		name string
		mode transfer.FileSourceMode
		// matrix of transport -> expected availability
		stdio, http, openai bool
	}{
		{"path", transfer.SourcePath, true, false, false},
		{"mint", transfer.SourceMint, false, true, false},
		{"url", transfer.SourceURL, false, false, true},
		{"data", transfer.SourceData, false, false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := transfer.UploadSource{Mode: tc.mode}
			require.Equal(t, tc.stdio, s.Available(transfer.TransportStdio), "stdio")
			require.Equal(t, tc.http, s.Available(transfer.TransportHTTP), "http")
			require.Equal(t, tc.openai, s.Available(transfer.TransportOpenAI), "openai")
		})
	}
}

func TestUploadSourceValidate(t *testing.T) {
	t.Run("rejects unknown mode", func(t *testing.T) {
		err := (transfer.UploadSource{Mode: "nope"}).Validate(transfer.TransportStdio)
		require.Error(t, err)
	})
	t.Run("path requires value and is stdio-only", func(t *testing.T) {
		require.Error(t, (transfer.UploadSource{Mode: transfer.SourcePath}).Validate(transfer.TransportStdio), "empty path")
		err := (transfer.UploadSource{Mode: transfer.SourcePath, Path: "/x"}).Validate(transfer.TransportStdio)
		require.NoError(t, err)
		require.Error(t, (transfer.UploadSource{Mode: transfer.SourcePath, Path: "/x"}).Validate(transfer.TransportHTTP))
	})
	t.Run("mint is http-only", func(t *testing.T) {
		s := transfer.UploadSource{Mode: transfer.SourceMint}
		require.NoError(t, s.Validate(transfer.TransportHTTP))
		require.Error(t, s.Validate(transfer.TransportStdio), "mint invalid in stdio")
		require.Error(t, s.Validate(transfer.TransportOpenAI), "mint invalid on openai tunnel")
	})
	t.Run("url requires value and is openai-only", func(t *testing.T) {
		require.Error(t, (transfer.UploadSource{Mode: transfer.SourceURL}).Validate(transfer.TransportOpenAI), "empty url")
		require.NoError(t, (transfer.UploadSource{Mode: transfer.SourceURL, URL: "https://x/y"}).Validate(transfer.TransportOpenAI))
		require.Error(t, (transfer.UploadSource{Mode: transfer.SourceURL, URL: "https://x/y"}).Validate(transfer.TransportHTTP))
	})
	t.Run("data requires value and is openai-only", func(t *testing.T) {
		require.Error(t, (transfer.UploadSource{Mode: transfer.SourceData}).Validate(transfer.TransportOpenAI), "empty data")
		require.NoError(t, (transfer.UploadSource{Mode: transfer.SourceData, Data: "data:;base64,AAAA"}).Validate(transfer.TransportOpenAI))
		require.Error(t, (transfer.UploadSource{Mode: transfer.SourceData, Data: "data:;base64,AAAA"}).Validate(transfer.TransportStdio))
	})
}

func TestRelayURLName(t *testing.T) {
	require.Equal(t, "report.pdf", transfer.RelayURLName("https://host/path/report.pdf"))
	require.Equal(t, "", transfer.RelayURLName("https://host/"))
	require.Equal(t, "", transfer.RelayURLName("not a url"))
}

func TestSourceResolverMintURL(t *testing.T) {
	cu := transfer.NewHTTPUpload(nil, 0)
	defer cu.Stop(context.Background())
	res := &transfer.SourceResolver{Transport: transfer.TransportHTTP, HTTPUpload: cu}

	url, err := res.MintURL(transfer.UploadSource{Mode: transfer.SourceMint}, "m.txt", time.Minute)
	require.NoError(t, err)
	require.NotEmpty(t, url, "minted URL non-empty")

	// Mint not valid off the HTTP transport.
	res2 := &transfer.SourceResolver{Transport: transfer.TransportStdio, HTTPUpload: cu}
	_, err = res2.MintURL(transfer.UploadSource{Mode: transfer.SourceMint}, "m.txt", time.Minute)
	require.Error(t, err, "mint invalid in stdio")

	// Missing coordinator.
	res3 := &transfer.SourceResolver{Transport: transfer.TransportHTTP}
	_, err = res3.MintURL(transfer.UploadSource{Mode: transfer.SourceMint}, "m.txt", time.Minute)
	require.Error(t, err, "missing coordinator")
}

func TestSourceResolverOpenBytesData(t *testing.T) {
	res := &transfer.SourceResolver{Transport: transfer.TransportOpenAI, RelayMaxBytes: 1 << 20}
	payload := []byte("hello vault")
	uri := "data:;name=note.txt;size=" + strconv.Itoa(len(payload)) + ";base64," + base64.StdEncoding.EncodeToString(payload)

	body, size, name, err := res.OpenBytes(context.Background(), transfer.UploadSource{Mode: transfer.SourceData, Data: uri})
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

	res := &transfer.SourceResolver{
		Transport:         transfer.TransportOpenAI,
		RelayMaxBytes:     1 << 20,
		RelayAllowedHosts: []string{"127.0.0.1"},
		RelayHTTPClient:   srv.Client(),
	}
	body, size, name, err := res.OpenBytes(context.Background(), transfer.UploadSource{Mode: transfer.SourceURL, URL: srv.URL + "/dir/file.txt"})
	require.NoError(t, err)
	defer body.Close()
	require.Equal(t, "file.txt", name)
	got, _ := io.ReadAll(body)
	require.Equal(t, "url bytes", string(got))
	require.Equal(t, int64(len(payload)), size)
}

func TestSourceResolverRejectsBadModeAndTransport(t *testing.T) {
	res := &transfer.SourceResolver{Transport: transfer.TransportHTTP}
	// path cannot OpenBytes (not a stream mode).
	_, _, _, err := res.OpenBytes(context.Background(), transfer.UploadSource{Mode: transfer.SourcePath, Path: "/x"})
	require.Error(t, err)
	// data not valid on the HTTP transport.
	_, _, _, err = res.OpenBytes(context.Background(), transfer.UploadSource{Mode: transfer.SourceData, Data: "data:;base64,AAAA"})
	require.Error(t, err)
}
