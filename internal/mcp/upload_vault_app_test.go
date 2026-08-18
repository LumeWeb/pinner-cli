package mcp

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"go.lumeweb.com/pinner-cli/internal/mcp/core/model"
)

// fakeVaultPutHandler is a controllable VaultPutHandler for app tests.
type fakeVaultPutHandler struct {
	gotVaultPath string
	gotBody      string
	err          error
}

func (f *fakeVaultPutHandler) Put(ctx context.Context, r io.Reader, _ int64, vaultPath string) (any, error) {
	buf, _ := io.ReadAll(r)
	f.gotVaultPath = vaultPath
	f.gotBody = string(buf)
	if f.err != nil {
		return nil, f.err
	}
	return map[string]any{"vault_path": vaultPath, "size": len(buf)}, nil
}

// buildVaultUploadAppServer constructs the catalog + server the way the adapter
// does, mirroring the production registration order in registerCustomTools:
// the vault_put_file descriptor is indexed in the catalog, the app view
// attaches _meta.ui to it, that meta is copied onto the descriptor served
// directly via RegisterOfficialDescriptor (the production surface), and a real
// vaultHTTPUpload coordinator backs the mint helper.
func buildVaultUploadAppServer(t *testing.T, fake *fakeVaultPutHandler) *mcp.Server {
	srv, _ := buildVaultUploadAppServerEx(t, fake)
	return srv
}

// buildVaultUploadAppServerEx is buildVaultUploadAppServer but also returns the
// coordinator so tests can configure it (e.g. AddTrustedOrigins).
func buildVaultUploadAppServerEx(t *testing.T, fake *fakeVaultPutHandler) (*mcp.Server, *vaultHTTPUpload) {
	t.Helper()
	if fake == nil {
		fake = &fakeVaultPutHandler{}
	}
	catalog := NewToolCatalog()
	srv := NewOfficialServer(nil)
	vu := NewVaultHTTPUpload(fake.Put, 1<<20)

	vaultPutDesc := NewVaultPutFileDescriptor(false, false, nil, vu, fake.Put, nil, 0)
	catalog.Add(model.ToolEntryFromDescriptor(vaultPutDesc))
	if err := RegisterVaultUploadApp(srv, catalog, vu); err != nil {
		t.Fatalf("RegisterVaultUploadApp: %v", err)
	}
	// Production copies the app-view _meta from the catalog entry onto the
	// descriptor served directly so the direct tools/list surface sees it.
	if entry, ok := catalog.Get("vault_put_file"); ok {
		if vaultPutDesc.Meta == nil {
			vaultPutDesc.Meta = map[string]any{}
		}
		for k, v := range entry.Meta {
			vaultPutDesc.Meta[k] = v
		}
	}
	if err := RegisterOfficialDescriptor(srv, vaultPutDesc); err != nil {
		t.Fatalf("RegisterOfficialDescriptor: %v", err)
	}
	if err := RegisterOfficialCuratedTools(srv, catalog); err != nil {
		t.Fatalf("RegisterOfficialCuratedTools: %v", err)
	}
	t.Cleanup(func() { vu.Stop(context.Background()) })
	return srv, vu
}

func TestRegisterVaultUploadAppWire(t *testing.T) {
	srv := buildVaultUploadAppServer(t, nil)
	cs := connectOfficialClient(t, srv)
	ctx := context.Background()

	res, err := cs.ListResources(ctx, nil)
	if err != nil {
		t.Fatalf("ListResources: %v", err)
	}
	var foundRes bool
	for _, r := range res.Resources {
		if r.URI == VaultUploadAppURI {
			foundRes = true
		}
	}
	if !foundRes {
		t.Fatalf("vault upload resource not listed; got %#v", res.Resources)
	}

	tres, err := cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	var putTool, submitTool *mcp.Tool
	for _, x := range tres.Tools {
		switch x.Name {
		case "vault_put_file":
			putTool = x
		case "vault_upload_submit":
			submitTool = x
		}
	}
	if putTool == nil {
		t.Fatalf("vault_put_file not listed")
	}
	ui, ok := putTool.Meta["ui"].(map[string]any)
	if !ok {
		t.Fatalf("_meta.ui missing on vault_put_file: %T", putTool.Meta["ui"])
	}
	if got := ui["resourceUri"]; got != VaultUploadAppURI {
		t.Fatalf("_meta.ui.resourceUri = %#v, want %q", got, VaultUploadAppURI)
	}

	if submitTool == nil {
		t.Fatalf("vault_upload_submit helper not registered")
	}
	stUI, ok := submitTool.Meta["ui"].(map[string]any)
	if !ok {
		t.Fatalf("_meta.ui missing on vault_upload_submit: %T", submitTool.Meta["ui"])
	}
	vis, ok := stUI["visibility"].([]any)
	if !ok {
		t.Fatalf("_meta.ui.visibility missing on vault_upload_submit: %T", stUI["visibility"])
	}
	if len(vis) != 1 || vis[0] != "app" {
		t.Fatalf("vault_upload_submit visibility = %#v, want [app]", vis)
	}
}

func TestVaultUploadHelperMintAndPut(t *testing.T) {
	fake := &fakeVaultPutHandler{}
	srv := buildVaultUploadAppServer(t, fake)
	cs := connectOfficialClient(t, srv)
	ctx := context.Background()

	// 1. The app mints a one-time presigned PUT endpoint via the helper. No
	// file bytes cross the tool channel — only the URL.
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name: "vault_upload_submit",
		Arguments: map[string]any{
			"vault_path": "vault:/uploads/report.pdf",
		},
	})
	require.NoError(t, err)
	require.False(t, res.IsError, "helper returned error: %s", requireText(t, res))
	var minted struct {
		URL string `json:"url"`
	}
	b, _ := json.Marshal(res.StructuredContent)
	_ = json.Unmarshal(b, &minted)
	require.Contains(t, minted.URL, "/vault-upload/", "minted URL should point at the vault-upload route")

	// 2. The iframe's Uppy XHR uploader PUTs the raw body (formData off)
	// straight to the minted endpoint, as a browser would.
	req, err := http.NewRequest(http.MethodPut, minted.URL, strings.NewReader("hello"))
	require.NoError(t, err)
	httpRes, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer httpRes.Body.Close()
	body, _ := io.ReadAll(httpRes.Body)
	require.Equal(t, http.StatusOK, httpRes.StatusCode, "PUT failed: %s", body)

	// 3. The fake vault handler received the exact bytes at the bound path.
	require.Equal(t, "vault:/uploads/report.pdf", fake.gotVaultPath)
	require.Equal(t, "hello", fake.gotBody)

	// 4. The response carries the vault result.
	require.Contains(t, string(body), "vault:/uploads/report.pdf")
}

// TestVaultUploadCORS verifies the minted vault-upload endpoint answers the
// browser's cross-origin OPTIONS preflight and restricts the reflected origin
// to the coordinator's own trusted base URL, so the app's Uppy XHR uploader
// can PUT while an arbitrary (untrusted) page is refused the CORS grant and
// cannot trigger a cross-origin write through the victim's browser.
func TestVaultUploadCORS(t *testing.T) {
	fake := &fakeVaultPutHandler{}
	srv := buildVaultUploadAppServer(t, fake)
	cs := connectOfficialClient(t, srv)

	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "vault_upload_submit",
		Arguments: map[string]any{"vault_path": "vault:/uploads/photo.png"},
	})
	require.NoError(t, err)
	require.False(t, res.IsError, "%s", requireText(t, res))
	var minted struct {
		URL string `json:"url"`
	}
	b, _ := json.Marshal(res.StructuredContent)
	_ = json.Unmarshal(b, &minted)

	// The trusted origin is the coordinator's own loopback origin (visible in
	// the minted URL). corsUpload only reflects an origin that matches this.
	u, err := url.Parse(minted.URL)
	require.NoError(t, err)
	trusted := u.Scheme + "://" + u.Host

	// Trusted origin preflight: must be answered with the reflecting origin,
	// allowed PUT, and no credentials.
	pre, err := http.NewRequest(http.MethodOptions, minted.URL, nil)
	require.NoError(t, err)
	pre.Header.Set("Origin", trusted)
	pre.Header.Set("Access-Control-Request-Method", "PUT")
	preResp, err := http.DefaultClient.Do(pre)
	require.NoError(t, err)
	preResp.Body.Close()
	require.Equal(t, http.StatusNoContent, preResp.StatusCode, "preflight status")
	require.Equal(t, trusted, preResp.Header.Get("Access-Control-Allow-Origin"))
	require.Contains(t, preResp.Header.Get("Access-Control-Allow-Methods"), "PUT")
	require.NotEqual(t, "true", preResp.Header.Get("Access-Control-Allow-Credentials"), "must not send credentials")

	// Untrusted origin: the response must NOT grant CORS, so the browser
	// refuses the cross-origin write.
	evil, err := http.NewRequest(http.MethodOptions, minted.URL, nil)
	require.NoError(t, err)
	evil.Header.Set("Origin", "https://evil.example.com")
	evil.Header.Set("Access-Control-Request-Method", "PUT")
	evilResp, err := http.DefaultClient.Do(evil)
	require.NoError(t, err)
	evilResp.Body.Close()
	require.Empty(t, evilResp.Header.Get("Access-Control-Allow-Origin"), "untrusted origin must not be reflected")

	// Trusted-origin PUT: cross-origin write succeeds and lands in the vault.
	put, err := http.NewRequest(http.MethodPut, minted.URL, strings.NewReader("cors body"))
	require.NoError(t, err)
	put.Header.Set("Origin", trusted)
	putResp, err := http.DefaultClient.Do(put)
	require.NoError(t, err)
	defer putResp.Body.Close()
	require.Equal(t, http.StatusOK, putResp.StatusCode)
	require.Equal(t, trusted, putResp.Header.Get("Access-Control-Allow-Origin"))
	require.Equal(t, "vault:/uploads/photo.png", fake.gotVaultPath)
	require.Equal(t, "cors body", fake.gotBody)
}

// TestVaultUploadCORSConfiguredHost verifies a configured MCP-host origin
// (added via AddTrustedOrigins) is reflected for the cross-origin Uppy PUT,
// while an arbitrary unlisted origin is still refused.
func TestVaultUploadCORSConfiguredHost(t *testing.T) {
	fake := &fakeVaultPutHandler{}
	srv, vu := buildVaultUploadAppServerEx(t, fake)
	vu.AddTrustedOrigins("https://apps.example.com")
	cs := connectOfficialClient(t, srv)

	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "vault_upload_submit",
		Arguments: map[string]any{"vault_path": "vault:/uploads/photo.png"},
	})
	require.NoError(t, err)
	require.False(t, res.IsError, "%s", requireText(t, res))
	var minted struct {
		URL string `json:"url"`
	}
	b, _ := json.Marshal(res.StructuredContent)
	_ = json.Unmarshal(b, &minted)

	// Configured host origin: reflected + allowed.
	pre, err := http.NewRequest(http.MethodOptions, minted.URL, nil)
	require.NoError(t, err)
	pre.Header.Set("Origin", "https://apps.example.com")
	pre.Header.Set("Access-Control-Request-Method", "PUT")
	preResp, err := http.DefaultClient.Do(pre)
	require.NoError(t, err)
	preResp.Body.Close()
	require.Equal(t, "https://apps.example.com", preResp.Header.Get("Access-Control-Allow-Origin"))

	// Arbitrary unlisted origin: still refused.
	evil, err := http.NewRequest(http.MethodOptions, minted.URL, nil)
	require.NoError(t, err)
	evil.Header.Set("Origin", "https://evil.example.com")
	evil.Header.Set("Access-Control-Request-Method", "PUT")
	evilResp, err := http.DefaultClient.Do(evil)
	require.NoError(t, err)
	evilResp.Body.Close()
	require.Empty(t, evilResp.Header.Get("Access-Control-Allow-Origin"), "unlisted origin must not be reflected")
}

func TestVaultUploadMintRejectsBadTTL(t *testing.T) {
	fake := &fakeVaultPutHandler{}
	srv := buildVaultUploadAppServer(t, fake)
	cs := connectOfficialClient(t, srv)

	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "vault_upload_submit",
		Arguments: map[string]any{"vault_path": "vault:/a", "ttl": "nope"},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if !res.IsError {
		t.Fatalf("invalid ttl must be rejected")
	}
}

func TestVaultUploadTokenIsSingleUse(t *testing.T) {
	fake := &fakeVaultPutHandler{}
	srv := buildVaultUploadAppServer(t, fake)
	cs := connectOfficialClient(t, srv)

	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "vault_upload_submit",
		Arguments: map[string]any{"vault_path": "vault:/uploads/one.pdf"},
	})
	require.NoError(t, err)
	require.False(t, res.IsError)
	var minted struct {
		URL string `json:"url"`
	}
	b, _ := json.Marshal(res.StructuredContent)
	_ = json.Unmarshal(b, &minted)

	httpReq := func() int {
		req, err := http.NewRequest(http.MethodPut, minted.URL, strings.NewReader("aaa"))
		require.NoError(t, err)
		r, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer r.Body.Close()
		_, _ = io.Copy(io.Discard, r.Body)
		return r.StatusCode
	}
	require.Equal(t, http.StatusOK, httpReq(), "first PUT should succeed")
	require.Equal(t, http.StatusNotFound, httpReq(), "re-PUT must be rejected (single-use token)")
}

func TestRegisterVaultUploadAppNilCoordinator(t *testing.T) {
	srv := NewOfficialServer(nil)
	catalog := NewToolCatalog()
	catalog.Add(&model.ToolEntry{Name: "vault_put_file", DirectVisible: true})
	if err := RegisterVaultUploadApp(srv, catalog, nil); err == nil {
		t.Fatalf("RegisterVaultUploadApp with nil coordinator must fail")
	}
}

// TestVaultUploadMintRejectsUnsafePath verifies the presigned-upload flow
// refuses to mint an endpoint for a destination that is not a well-formed
// vault FILE path: directories and traversal paths are all rejected before
// any token exists. Any well-formed vault file path (including paths outside
// a single "uploads" folder) is an allowed mint destination — there is no
// folder restriction.
func TestVaultUploadMintRejectsUnsafePath(t *testing.T) {
	fake := &fakeVaultPutHandler{}
	srv := buildVaultUploadAppServer(t, fake)
	cs := connectOfficialClient(t, srv)

	badPaths := []string{
		"vault:/uploads/",                // directory (trailing slash)
		"vault:/uploads/../../secret.db", // traversal
		"vault:/uploads/..",              // .. as the leaf filename
		"vault:/uploads/.",               // . as the leaf filename
		"vault://work/uploads/x.pdf",     // profile-authority path unsupported
		"not-a-vault-path",               // not a vault: path
	}
	for _, p := range badPaths {
		res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
			Name:      "vault_upload_submit",
			Arguments: map[string]any{"vault_path": p},
		})
		if err != nil {
			t.Fatalf("CallTool(%q): %v", p, err)
		}
		if !res.IsError {
			t.Errorf("vault path %q must be rejected, but mint succeeded", p)
		}
	}
	// The handler must never have been offered any rejected path.
	require.Equal(t, "", fake.gotVaultPath)
}
