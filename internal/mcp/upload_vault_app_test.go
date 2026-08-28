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
	"go.lumeweb.com/pinner-cli/internal/mcp/core/transfer"
	"go.lumeweb.com/pinner-cli/internal/mcp/sdk"
	"go.lumeweb.com/pinner-cli/internal/mcp/upload"
	"go.lumeweb.com/pinner-cli/internal/mcp/vault"
)

// fakeVaultPutHandler is a controllable vault.VaultPutHandler for app tests.
type fakeVaultPutHandler struct {
	gotVaultPath string
	gotBody      string
	err          error
}

func (f *fakeVaultPutHandler) Put(ctx context.Context, r io.Reader, _ int64, vaultPath string, _ map[string]any) (any, error) {
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
// transfer.VaultHTTPUpload coordinator backs the mint helper.
func buildVaultUploadAppServer(t *testing.T, fake *fakeVaultPutHandler) *mcp.Server {
	srv, _ := buildVaultUploadAppServerEx(t, fake)
	return srv
}

// buildVaultUploadAppServerEx is buildVaultUploadAppServer but also returns the
// coordinator so tests can configure it (e.g. AddTrustedOrigins).
func buildVaultUploadAppServerEx(t *testing.T, fake *fakeVaultPutHandler) (*mcp.Server, *transfer.VaultHTTPUpload) {
	t.Helper()
	if fake == nil {
		fake = &fakeVaultPutHandler{}
	}
	catalog := NewToolCatalog()
	srv := sdk.NewServer(nil)
	vu := transfer.NewVaultHTTPUpload(fake.Put, 1<<20)

	vaultPutDesc := vault.NewVaultPutFileDescriptor(transportFeatures(false, false), false, false, nil, vu, fake.Put, nil, 0)
	catalog.Add(model.ToolEntryFromDescriptor(vaultPutDesc))
	// Seed the launcher exactly as registerOpenLauncher does in production;
	// the app's AttachTo now points at open_vault_manager, not vault_put_file.
	seedLauncherForTest(t, srv, catalog, upload.OpenVaultManagerToolName, upload.VaultUploadAppURI, model.CategoryVault)
	if err := upload.RegisterVaultUploadApp(srv, catalog, vu); err != nil {
		t.Fatalf("upload.RegisterVaultUploadApp: %v", err)
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
		if r.URI == upload.VaultUploadAppURI {
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
	var putTool, submitTool, launcherTool *mcp.Tool
	for _, x := range tres.Tools {
		switch x.Name {
		case "vault_put_file":
			putTool = x
		case "vault_upload_submit":
			submitTool = x
		case upload.OpenVaultManagerToolName:
			launcherTool = x
		}
	}
	if putTool == nil {
		t.Fatalf("vault_put_file not listed")
	}
	// vault_put_file is a headless primitive: no ui.resourceUri.
	if ui, ok := putTool.Meta["ui"].(map[string]any); ok {
		if _, has := ui["resourceUri"]; has {
			t.Fatalf("vault_put_file must NOT carry ui.resourceUri (headless); got %v", ui["resourceUri"])
		}
	}
	// open_vault_manager is the ONLY tool carrying resourceUri.
	if launcherTool == nil {
		t.Fatalf("open_vault_manager not listed")
	}
	lui, ok := launcherTool.Meta["ui"].(map[string]any)
	if !ok {
		t.Fatalf("_meta.ui missing on open_vault_manager: %T", launcherTool.Meta["ui"])
	}
	if got := lui["resourceUri"]; got != upload.VaultUploadAppURI {
		t.Fatalf("open_vault_manager _meta.ui.resourceUri = %#v, want %q", got, upload.VaultUploadAppURI)
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

	// A dynamic, non-loopback sandbox origin is also reflected (token-gated
	// route), which is what unblocks a host-rendered app iframe.
	evil, err := http.NewRequest(http.MethodOptions, minted.URL, nil)
	require.NoError(t, err)
	evil.Header.Set("Origin", "https://a1b2c3d4e5f6g7h8.host-sandbox.example")
	evil.Header.Set("Access-Control-Request-Method", "PUT")
	evilResp, err := http.DefaultClient.Do(evil)
	require.NoError(t, err)
	evilResp.Body.Close()
	require.Equal(t, "https://a1b2c3d4e5f6g7h8.host-sandbox.example", evilResp.Header.Get("Access-Control-Allow-Origin"), "dynamic sandbox origin must be reflected")

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

// TestVaultUploadCORSConfiguredHost verifies a configured MCP-host origin and a
// dynamic, non-loopback sandbox origin are both reflected for the cross-origin
// Uppy PUT (the route is token-gated, so it reflects any origin).
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

	// A dynamic, unlisted sandbox origin is also reflected (token-gated route).
	evil, err := http.NewRequest(http.MethodOptions, minted.URL, nil)
	require.NoError(t, err)
	evil.Header.Set("Origin", "https://a1b2c3d4e5f6g7h8.host-sandbox.example")
	evil.Header.Set("Access-Control-Request-Method", "PUT")
	evilResp, err := http.DefaultClient.Do(evil)
	require.NoError(t, err)
	evilResp.Body.Close()
	require.Equal(t, "https://a1b2c3d4e5f6g7h8.host-sandbox.example", evilResp.Header.Get("Access-Control-Allow-Origin"), "dynamic sandbox origin must be reflected")
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
	srv := sdk.NewServer(nil)
	catalog := NewToolCatalog()
	catalog.Add(&model.ToolEntry{Name: "vault_put_file", DirectVisible: true})
	if err := upload.RegisterVaultUploadApp(srv, catalog, nil); err == nil {
		t.Fatalf("upload.RegisterVaultUploadApp with nil coordinator must fail")
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

// TestVaultUploadResourceAdvertisesConnectDomains is the vault twin of
// TestIPFSUploadResourceAdvertisesConnectDomains: the vault app resource must
// advertise its presigned /vault-upload/<token> origin in read-level
// _meta.ui.csp.connectDomains so a host sandbox CSP permits the app's
// cross-origin Uppy XHR PUT. The origin is resolved dynamically at read time
// because the tunnel/base URL or loopback address is only known after the
// server is up.
func TestVaultUploadResourceAdvertisesConnectDomains(t *testing.T) {
	srv, vu := buildVaultUploadAppServerEx(t, nil)
	cs := connectOfficialClient(t, srv)

	res, err := cs.ReadResource(context.Background(), &mcp.ReadResourceParams{URI: upload.VaultUploadAppURI})
	require.NoError(t, err)
	require.NotEmpty(t, res.Contents, "read returned no content items")
	// _meta.ui is on the content item, not the result root (ext-apps spec).
	ui, ok := res.Contents[0].Meta["ui"].(map[string]any)
	require.True(t, ok, "read content-item _meta.ui missing: %#v", res.Contents[0].Meta)
	csp, ok := ui["csp"].(map[string]any)
	require.True(t, ok, "read result _meta.ui.csp missing: %#v", ui)
	cd, ok := csp["connectDomains"].([]any)
	require.True(t, ok, "read result _meta.ui.csp.connectDomains missing: %#v", csp)
	require.NotEmpty(t, cd, "connectDomains must not be empty")

	// Loopback mode: the advertised connect domain is the live loopback origin.
	want := vu.ConnectOrigins()[0]
	require.Equal(t, want, cd[0], "loopback connectDomains[0]")

	// HTTP/tunnel mode: once the coordinator's base URL is resolved (mirroring
	// serveHTTP's SetBaseURL), a subsequent read advertises that origin.
	vu.SetBaseURL("https://tunnel.example.com")
	res2, err := cs.ReadResource(context.Background(), &mcp.ReadResourceParams{URI: upload.VaultUploadAppURI})
	require.NoError(t, err)
	require.NotEmpty(t, res2.Contents, "read returned no content items")
	ui2 := res2.Contents[0].Meta["ui"].(map[string]any)
	cd2 := ui2["csp"].(map[string]any)["connectDomains"].([]any)
	require.Equal(t, "https://tunnel.example.com", cd2[0], "tunnel connectDomains[0]")
}
