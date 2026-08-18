package ieo

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateChatGPTFileReference(t *testing.T) {
	valid := ChatGPTFileReference{DownloadURL: "https://files.openai.com/file", FileID: "file_123", FileName: "report.pdf"}
	require.NoError(t, ValidateChatGPTFileReference(valid, 1024))

	for name, ref := range map[string]ChatGPTFileReference{
		"missing id":  {DownloadURL: valid.DownloadURL},
		"missing url": {FileID: valid.FileID},
		"http":        {DownloadURL: "http://files.openai.com/file", FileID: valid.FileID},
		"userinfo":    {DownloadURL: "https://alice@files.openai.com/file", FileID: valid.FileID},
		"path":        {DownloadURL: valid.DownloadURL, FileID: valid.FileID, FileName: "../report.pdf"},
	} {
		t.Run(name, func(t *testing.T) {
			require.ErrorIs(t, ValidateChatGPTFileReference(ref, 1024), ErrInvalidFileReference)
		})
	}
}

func TestOpenChatGPTFile(t *testing.T) {
	t.Run("streams allowed file", func(t *testing.T) {
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Length", "5")
			_, _ = w.Write([]byte("hello"))
		}))
		defer server.Close()

		client := server.Client()
		ref := ChatGPTFileReference{DownloadURL: server.URL + "/file", FileID: "file_123"}
		body, size, err := OpenChatGPTFile(context.Background(), ref, FileRelayOptions{
			HTTPClient:   client,
			AllowedHosts: []string{"127.0.0.1"},
			MaxBytes:     5,
		})
		require.NoError(t, err)
		defer body.Close()
		require.Equal(t, int64(5), size)
		data, err := io.ReadAll(body)
		require.NoError(t, err)
		require.Equal(t, "hello", string(data))
	})

	t.Run("rejects disallowed host", func(t *testing.T) {
		server := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
		defer server.Close()
		_, _, err := OpenChatGPTFile(context.Background(), ChatGPTFileReference{
			DownloadURL: server.URL,
			FileID:      "file_123",
		}, FileRelayOptions{HTTPClient: server.Client(), AllowedHosts: []string{"example.com"}})
		require.ErrorIs(t, err, ErrInvalidFileReference)
	})

	t.Run("rejects declared oversized file", func(t *testing.T) {
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Length", "10")
		}))
		defer server.Close()
		_, _, err := OpenChatGPTFile(context.Background(), ChatGPTFileReference{
			DownloadURL: server.URL,
			FileID:      "file_123",
		}, FileRelayOptions{HTTPClient: server.Client(), AllowedHosts: []string{"127.0.0.1"}, MaxBytes: 5})
		require.ErrorIs(t, err, ErrFileTooLarge)
	})

	t.Run("rejects body that exceeds limit", func(t *testing.T) {
		reader := &limitedReadCloser{ReadCloser: io.NopCloser(strings.NewReader("toolarge")), remaining: 5}
		_, err := io.ReadAll(reader)
		require.ErrorIs(t, err, ErrFileTooLarge)
	})

	t.Run("rejects unknown content length", func(t *testing.T) {
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Transfer-Encoding", "chunked")
			_, _ = w.Write([]byte("hello"))
		}))
		defer server.Close()
		_, _, err := OpenChatGPTFile(context.Background(), ChatGPTFileReference{
			DownloadURL: server.URL,
			FileID:      "file_123",
		}, FileRelayOptions{HTTPClient: server.Client(), AllowedHosts: []string{"127.0.0.1"}, MaxBytes: 5})
		require.ErrorIs(t, err, ErrInvalidFileReference)
	})

	t.Run("refuses private-ip dial on default transport", func(t *testing.T) {
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte("hello"))
		}))
		defer server.Close()
		// No injected client => Pinner owns the transport, so the SSRF dial
		// guard must reject the loopback (private) address even though the
		// hostname passes the string allowlist.
		_, _, err := OpenChatGPTFile(context.Background(), ChatGPTFileReference{
			DownloadURL: server.URL,
			FileID:      "file_123",
		}, FileRelayOptions{AllowedHosts: []string{"127.0.0.1"}, MaxBytes: 5})
		require.ErrorContains(t, err, "refusing to dial non-public address")
	})
}

func TestHostAllowed(t *testing.T) {
	require.True(t, hostAllowed("files.openai.com", []string{"openai.com"}))
	require.False(t, hostAllowed("openai.com.attacker.test", []string{"openai.com"}))
}

func TestIsPrivateIP(t *testing.T) {
	// Standard private / special ranges netip already flags.
	for _, cidr := range []string{"127.0.0.1", "10.0.0.1", "192.168.1.1", "172.16.5.5", "169.254.10.1", "::1"} {
		require.Truef(t, isPrivateIP(netip.MustParseAddr(cidr)), "expected %s to be denied", cidr)
	}
	// Special-use / non-global ranges not classed private by netip.IsPrivate.
	for _, cidr := range []string{"100.64.0.1", "100.127.255.254", "192.0.0.5", "198.18.0.1", "198.19.255.254", "192.88.99.1"} {
		require.Truef(t, isPrivateIP(netip.MustParseAddr(cidr)), "expected %s to be denied", cidr)
	}
	// IPv4-in-IPv6 mapped forms of the above must also be denied (bypass guard).
	for _, cidr := range []string{"::ffff:100.64.0.1", "::ffff:192.168.1.1", "::ffff:10.0.0.1", "::ffff:198.18.0.1"} {
		require.Truef(t, isPrivateIP(netip.MustParseAddr(cidr)), "expected mapped %s to be denied", cidr)
	}
	// The deprecated IPv4-compatible IPv6 form (::a.b.c.d) must also be denied;
	// netip does not unmap or classify it, so a bare conversion is required.
	for _, cidr := range []string{"::7f00:1", "::a00:1", "::c0a8:101", "::6440:1", "::c000:5"} {
		require.Truef(t, isPrivateIP(netip.MustParseAddr(cidr)), "expected IPv4-compatible %s to be denied", cidr)
	}
	// Public addresses must remain allowed.
	for _, ip := range []string{"8.8.8.8", "1.1.1.1", "93.184.216.34", "2606:4700:4700::1111"} {
		require.Falsef(t, isPrivateIP(netip.MustParseAddr(ip)), "expected %s to be allowed", ip)
	}
}

func TestLimitedReadCloserDoesNotHideOverflow(t *testing.T) {
	reader := &limitedReadCloser{ReadCloser: io.NopCloser(strings.NewReader("123456")), remaining: 5}
	_, err := io.ReadAll(reader)
	require.ErrorIs(t, err, ErrFileTooLarge)
	require.True(t, errors.Is(err, ErrFileTooLarge))
}
