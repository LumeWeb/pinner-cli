package mcp

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"go.lumeweb.com/pinner-cli/internal/mcp/core/model"
	"go.lumeweb.com/pinner-cli/internal/mcp/core/transfer"
	"go.lumeweb.com/pinner-cli/internal/mcp/sdk"
	"go.lumeweb.com/pinner-cli/internal/mcp/upload"
)

// buildIPFSUploadAppServer constructs the catalog + server the way the adapter
// does: the upload_file descriptor is indexed in the catalog, the app view
// attaches _meta.ui to it, that meta is copied onto the descriptor served
// directly via RegisterOfficialDescriptor (the production surface), and a real
// presigned transfer.Upload coordinator backs the mint/poll helpers.
func buildIPFSUploadAppServer(t *testing.T) (*mcp.Server, *transfer.Upload) {
	t.Helper()

	mgr := transfer.NewUploadTaskManager(func(_ context.Context, reader io.Reader, _ int64, name string, _ bool, _ string, _ bool) (any, error) {
		_, _ = io.Copy(io.Discard, reader)
		return map[string]any{"cid": "QmApp", "name": name}, nil
	}, 0)
	cu := transfer.NewHTTPUpload(mgr, 1<<20)
	t.Cleanup(func() { cu.Stop(context.Background()) })

	catalog := NewToolCatalog()
	srv := sdk.NewServer(nil)

	// Mirror the production registration sequence in registerCustomTools.
	uploadFileDesc := model.ToolDescriptor{
		Name:        "upload_file",
		Title:       "Upload to IPFS",
		Description: "Upload a file to Pinner over IPFS.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{}}`),
		Handler: func(_ context.Context, _ model.ToolRequest) (model.ToolResult, error) {
			return model.ToolResult{Text: "ok"}, nil
		},
	}
	catalog.Add(model.ToolEntryFromDescriptor(uploadFileDesc))
	// Seed the launcher exactly as registerOpenLauncher does in production;
	// the app's AttachTo now points at open_upload_manager, not upload_file.
	seedLauncherForTest(t, srv, catalog, upload.OpenUploadManagerToolName, upload.OpenUploadManagerURI, model.CategoryCore)
	if err := upload.RegisterIPFSUploadApp(srv, catalog, cu); err != nil {
		t.Fatalf("upload.RegisterIPFSUploadApp: %v", err)
	}
	if err := RegisterOfficialDescriptor(srv, uploadFileDesc); err != nil {
		t.Fatalf("RegisterOfficialDescriptor: %v", err)
	}
	if err := RegisterOfficialCuratedTools(srv, catalog); err != nil {
		t.Fatalf("RegisterOfficialCuratedTools: %v", err)
	}
	return srv, cu
}

func TestRegisterIPFSUploadAppWire(t *testing.T) {
	srv, _ := buildIPFSUploadAppServer(t)
	cs := connectOfficialClient(t, srv)
	ctx := context.Background()

	res, err := cs.ListResources(ctx, nil)
	if err != nil {
		t.Fatalf("ListResources: %v", err)
	}
	var foundRes bool
	for _, r := range res.Resources {
		if r.URI == upload.IPFSUploadAppURI {
			foundRes = true
		}
	}
	if !foundRes {
		t.Fatalf("ipfs upload resource not listed; got %#v", res.Resources)
	}

	tres, err := cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	var upTool, launcherTool, mintTool, pollTool *mcp.Tool
	for _, x := range tres.Tools {
		switch x.Name {
		case "upload_file":
			upTool = x
		case "open_upload_manager":
			launcherTool = x
		case "ipfs_upload_submit":
			mintTool = x
		case "ipfs_upload_status":
			pollTool = x
		}
	}
	if upTool == nil {
		t.Fatalf("upload_file not listed")
	}
	// upload_file is a HEADLESS primitive: it must NOT carry ui.resourceUri so
	// mid-workflow agent calls never render a card.
	if ui, ok := upTool.Meta["ui"].(map[string]any); ok {
		if _, has := ui["resourceUri"]; has {
			t.Fatalf("upload_file must NOT carry ui.resourceUri (got %v); it is a headless primitive", ui["resourceUri"])
		}
	}
	// open_upload_manager is the ONLY tool carrying the resourceUri.
	if launcherTool == nil {
		t.Fatalf("open_upload_manager not listed")
	}
	lui, ok := launcherTool.Meta["ui"].(map[string]any)
	if !ok {
		t.Fatalf("_meta.ui missing on open_upload_manager: %T", launcherTool.Meta["ui"])
	}
	if got := lui["resourceUri"]; got != upload.IPFSUploadAppURI {
		t.Fatalf("open_upload_manager _meta.ui.resourceUri = %#v, want %q", got, upload.IPFSUploadAppURI)
	}
	vis, _ := lui["visibility"].([]any)
	if len(vis) != 2 {
		t.Fatalf("open_upload_manager visibility = %#v, want [model app]", vis)
	}

	if mintTool == nil || pollTool == nil {
		t.Fatalf("app helpers not registered: submit=%v status=%v", mintTool != nil, pollTool != nil)
	}
	for _, helper := range []*mcp.Tool{mintTool, pollTool} {
		stUI, ok := helper.Meta["ui"].(map[string]any)
		if !ok {
			t.Fatalf("_meta.ui missing on %s: %T", helper.Name, helper.Meta["ui"])
		}
		vis, ok := stUI["visibility"].([]any)
		if !ok {
			t.Fatalf("_meta.ui.visibility missing on %s: %T", helper.Name, stUI["visibility"])
		}
		if len(vis) != 1 || vis[0] != "app" {
			t.Fatalf("%s visibility = %#v, want [app]", helper.Name, vis)
		}
	}
}

// TestIPFSUploadMintHelper verifies ipfs_upload_submit mints a real presigned
// endpoint URL (no file bytes cross the tool) and returns it, and that an
// invalid TTL is rejected.
func TestIPFSUploadMintHelper(t *testing.T) {
	srv, _ := buildIPFSUploadAppServer(t)
	cs := connectOfficialClient(t, srv)

	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "ipfs_upload_submit",
		Arguments: map[string]any{"name": "pic.png", "ttl": "1m"},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.IsError {
		t.Fatalf("mint helper returned error: %s", requireText(t, res))
	}
	sc, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(sc), "/upload/") {
		t.Fatalf("mint did not return a presigned URL: %s", sc)
	}

	// Invalid TTL is rejected (surfaced as an error result, not a Go error).
	badRes, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "ipfs_upload_submit",
		Arguments: map[string]any{"ttl": "nope"},
	})
	if err != nil {
		t.Fatalf("unexpected transport error: %v", err)
	}
	if !badRes.IsError {
		t.Fatalf("invalid ttl must fail; got: %s", requireText(t, badRes))
	}
}

// TestIPFSUploadPollHelper drives a real presigned PUT (as the Uppy XHR
// uploader would), reads the upload_handle from the 202 body, and confirms
// ipfs_upload_status surfaces the shared UploadTaskManager state.
func TestIPFSUploadPollHelper(t *testing.T) {
	srv, cu := buildIPFSUploadAppServer(t)
	cs := connectOfficialClient(t, srv)

	mintRes, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "ipfs_upload_submit",
		Arguments: map[string]any{"name": "doc.txt"},
	})
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	b, _ := json.Marshal(mintRes.StructuredContent)
	var mintSC map[string]any
	_ = json.Unmarshal(b, &mintSC)
	url, _ := mintSC["url"].(string)
	if !strings.Contains(url, "/upload/") {
		t.Fatalf("no minted URL: %s", b)
	}

	// PUT the file bytes like Uppy would.
	req, err := http.NewRequest(http.MethodPut, url, strings.NewReader("uppy xhr body"))
	if err != nil {
		t.Fatalf("PUT req: %v", err)
	}
	putResp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PUT: %v", err)
	}
	defer putResp.Body.Close()
	if putResp.StatusCode != http.StatusAccepted {
		t.Fatalf("PUT status = %d, want 202", putResp.StatusCode)
	}
	var putOut map[string]any
	if err := json.NewDecoder(putResp.Body).Decode(&putOut); err != nil {
		t.Fatalf("decode 202: %v", err)
	}
	handle, _ := putOut["upload_handle"].(string)
	if handle == "" {
		t.Fatalf("202 did not carry upload_handle: %v", putOut)
	}

	// Poll via the app helper until completed.
	deadline := time.Now().Add(2 * time.Second)
	for {
		pollRes, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
			Name:      "ipfs_upload_status",
			Arguments: map[string]any{"handle": handle},
		})
		if err != nil {
			t.Fatalf("poll: %v", err)
		}
		if s, ok := taskStateOf(pollRes); ok && s == transfer.UploadStateCompleted {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("upload did not complete in time")
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Bogus handle is rejected (error result, not a Go error).
	badRes, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "ipfs_upload_status",
		Arguments: map[string]any{"handle": "does-not-exist"},
	})
	if err != nil {
		t.Fatalf("unexpected transport error: %v", err)
	}
	if !badRes.IsError {
		t.Fatalf("bogus handle must fail; got: %s", requireText(t, badRes))
	}
	_ = cu
}

func TestRegisterIPFSUploadAppNilCoordinator(t *testing.T) {
	srv := sdk.NewServer(nil)
	catalog := NewToolCatalog()
	if err := upload.RegisterIPFSUploadApp(srv, catalog, nil); err == nil {
		t.Fatalf("upload.RegisterIPFSUploadApp with nil coordinator must fail")
	}
}

// TestIPFSUploadCORS verifies the minted presigned endpoint answers the
// browser's cross-origin OPTIONS preflight and restricts the reflected origin
// to the coordinator's own trusted base/loopback origin. Without CORS the
// browser rejects the cross-origin PUT before it ever reaches the server; with
// an origin allowlist, an arbitrary (untrusted) page is refused the grant and
// cannot trigger a cross-origin write through the victim's browser even if it
// knows a token.
func TestIPFSUploadCORS(t *testing.T) {
	srv, cu := buildIPFSUploadAppServer(t)
	cs := connectOfficialClient(t, srv)

	mintRes, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "ipfs_upload_submit",
		Arguments: map[string]any{"name": "pic.png"},
	})
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	b, _ := json.Marshal(mintRes.StructuredContent)
	var mintSC map[string]any
	_ = json.Unmarshal(b, &mintSC)
	urlVal, _ := mintSC["url"].(string)

	// The trusted origin is the coordinator's own loopback origin (visible in
	// the minted URL); corsUpload only reflects an origin that matches it.
	u, err := url.Parse(urlVal)
	if err != nil {
		t.Fatalf("parse minted url: %v", err)
	}
	trusted := u.Scheme + "://" + u.Host

	// Trusted-origin preflight: must be answered with the reflecting origin,
	// allowed PUT, and no credentials.
	pre, err := http.NewRequest(http.MethodOptions, urlVal, nil)
	if err != nil {
		t.Fatalf("preflight req: %v", err)
	}
	pre.Header.Set("Origin", trusted)
	pre.Header.Set("Access-Control-Request-Method", "PUT")
	preResp, err := http.DefaultClient.Do(pre)
	if err != nil {
		t.Fatalf("preflight: %v", err)
	}
	preResp.Body.Close()
	if preResp.StatusCode != http.StatusNoContent {
		t.Fatalf("preflight status = %d, want 204", preResp.StatusCode)
	}
	if aoc := preResp.Header.Get("Access-Control-Allow-Origin"); aoc != trusted {
		t.Fatalf("preflight Allow-Origin = %q, want %q", aoc, trusted)
	}
	if !strings.Contains(preResp.Header.Get("Access-Control-Allow-Methods"), "PUT") {
		t.Fatalf("preflight Allow-Methods missing PUT: %q", preResp.Header.Get("Access-Control-Allow-Methods"))
	}
	if cc := preResp.Header.Get("Access-Control-Allow-Credentials"); cc == "true" {
		t.Fatalf("cross-origin upload must not send credentials")
	}

	// A non-loopback, non-trusted dynamic origin (a stand-in for the MCP host's
	// per-session sandbox origin) is ALSO reflected: these routes are
	// token-gated, so reflecting any origin is safe and is what unblocks a
	// host-rendered app iframe.
	evil, err := http.NewRequest(http.MethodOptions, urlVal, nil)
	if err != nil {
		t.Fatalf("dynamic-origin preflight req: %v", err)
	}
	evil.Header.Set("Origin", "https://a1b2c3d4e5f6g7h8.host-sandbox.example")
	evil.Header.Set("Access-Control-Request-Method", "PUT")
	evilResp, err := http.DefaultClient.Do(evil)
	if err != nil {
		t.Fatalf("dynamic-origin preflight: %v", err)
	}
	evilResp.Body.Close()
	if aoc := evilResp.Header.Get("Access-Control-Allow-Origin"); aoc != "https://a1b2c3d4e5f6g7h8.host-sandbox.example" {
		t.Fatalf("dynamic origin not reflected: Allow-Origin = %q", aoc)
	}

	// The trusted-origin PUT must carry the reflecting origin header and
	// succeed.
	put, err := http.NewRequest(http.MethodPut, urlVal, strings.NewReader("cors body"))
	if err != nil {
		t.Fatalf("PUT req: %v", err)
	}
	put.Header.Set("Origin", trusted)
	putResp, err := http.DefaultClient.Do(put)
	if err != nil {
		t.Fatalf("PUT: %v", err)
	}
	defer putResp.Body.Close()
	if putResp.StatusCode != http.StatusAccepted {
		t.Fatalf("PUT status = %d, want 202", putResp.StatusCode)
	}
	if aoc := putResp.Header.Get("Access-Control-Allow-Origin"); aoc != trusted {
		t.Fatalf("PUT Allow-Origin = %q, want %q", aoc, trusted)
	}
	_ = cu
}

// TestIPFSUploadCORSOpaqueNull is the regression test for the CORS issue: an
// MCP host renders the upload app inside a sandboxed double-iframe whose
// Origin (the opaque origin, serialized as the literal string "null") is
// neither the server's own origin nor a configured trusted origin, yet the
// app's Uppy XHR uploader MUST be able to PUT the presigned /upload/<token>
// endpoint or the browser blocks it with "No 'Access-Control-Allow-Origin'
// header". The token-gated route must reflect the opaque origin while still
// refusing an arbitrary attacker origin.
func TestIPFSUploadCORSOpaqueNull(t *testing.T) {
	srv, cu := buildIPFSUploadAppServer(t)
	t.Cleanup(func() { cu.Stop(context.Background()) })
	cs := connectOfficialClient(t, srv)

	mintRes, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "ipfs_upload_submit",
		Arguments: map[string]any{"name": "pic.png"},
	})
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	b, _ := json.Marshal(mintRes.StructuredContent)
	var mintSC map[string]any
	_ = json.Unmarshal(b, &mintSC)
	url, _ := mintSC["url"].(string)

	const opaque = "null"

	// Preflight from the opaque sandbox origin: reflected + allowed PUT.
	pre, err := http.NewRequest(http.MethodOptions, url, nil)
	if err != nil {
		t.Fatalf("preflight req: %v", err)
	}
	pre.Header.Set("Origin", opaque)
	pre.Header.Set("Access-Control-Request-Method", "PUT")
	preResp, err := http.DefaultClient.Do(pre)
	if err != nil {
		t.Fatalf("preflight: %v", err)
	}
	preResp.Body.Close()
	if aoc := preResp.Header.Get("Access-Control-Allow-Origin"); aoc != opaque {
		t.Fatalf("opaque-origin preflight Allow-Origin = %q, want %q", aoc, opaque)
	}
	if cc := preResp.Header.Get("Access-Control-Allow-Credentials"); cc == "true" {
		t.Fatalf("opaque-origin upload must not send credentials")
	}

	// The actual PUT from the opaque origin must succeed (202) and land in the
	// async upload manager.
	put, err := http.NewRequest(http.MethodPut, url, strings.NewReader("opaque body"))
	if err != nil {
		t.Fatalf("PUT req: %v", err)
	}
	put.Header.Set("Origin", opaque)
	putResp, err := http.DefaultClient.Do(put)
	if err != nil {
		t.Fatalf("PUT: %v", err)
	}
	defer putResp.Body.Close()
	if putResp.StatusCode != http.StatusAccepted {
		t.Fatalf("PUT status = %d, want 202", putResp.StatusCode)
	}
	if aoc := putResp.Header.Get("Access-Control-Allow-Origin"); aoc != opaque {
		t.Fatalf("PUT Allow-Origin = %q, want %q", aoc, opaque)
	}

	// A dynamic, non-loopback sandbox origin is also reflected — the token-gated
	// route reflects any origin (see TestIPFSUploadCORS), which is what unblocks
	// a host-rendered app iframe whose sandbox origin the server cannot
	// pre-enumerate.
	evil, err := http.NewRequest(http.MethodOptions, url, nil)
	if err != nil {
		t.Fatalf("dynamic-origin preflight req: %v", err)
	}
	evil.Header.Set("Origin", "https://a1b2c3d4e5f6g7h8.host-sandbox.example")
	evil.Header.Set("Access-Control-Request-Method", "PUT")
	evilResp, err := http.DefaultClient.Do(evil)
	if err != nil {
		t.Fatalf("dynamic-origin preflight: %v", err)
	}
	evilResp.Body.Close()
	if aoc := evilResp.Header.Get("Access-Control-Allow-Origin"); aoc != "https://a1b2c3d4e5f6g7h8.host-sandbox.example" {
		t.Fatalf("dynamic origin not reflected: Allow-Origin = %q", aoc)
	}
}

// buildIPFSUploadSharedServer constructs a server that mirrors the PRODUCTION
// wiring for the shared upload operation: the REAL transport-aware upload_file
// descriptor (HTTP/tunnel mint mode) plus the "Upload to IPFS" App (its
// helpers) plus the model-facing async upload tools (upload_status/list) — all
// backed by the SAME Upload coordinator and SAME UploadTaskManager. This is the
// surface a single process serves, which is exactly what lets the model path
// (upload_file → upload_status) and the App path (ipfs_upload_submit →
// ipfs_upload_status) converge on one canonical handle.
func buildIPFSUploadSharedServer(t *testing.T) (*mcp.Server, *transfer.Upload, *transfer.UploadTaskManager) {
	t.Helper()

	var gotBytes atomic.Value
	mgr := transfer.NewUploadTaskManager(func(_ context.Context, reader io.Reader, _ int64, name string, _ bool, _ string, _ bool) (any, error) {
		b, _ := io.ReadAll(reader)
		gotBytes.Store(string(b))
		return map[string]any{"cid": "QmShared", "name": name}, nil
	}, 0)
	cu := transfer.NewHTTPUpload(mgr, 1<<20)
	t.Cleanup(func() { cu.Stop(context.Background()) })

	catalog := NewToolCatalog()
	srv := sdk.NewServer(nil)

	// The real HTTP/tunnel upload_file descriptor (mirrors custom_tools.go).
	uploadFileDesc := transfer.NewUploadFileDescriptor(false, false, nil, cu, nil, nil, 0)
	catalog.Add(model.ToolEntryFromDescriptor(uploadFileDesc))
	// Seed the launcher; the app's AttachTo now points at open_upload_manager.
	seedLauncherForTest(t, srv, catalog, upload.OpenUploadManagerToolName, upload.OpenUploadManagerURI, model.CategoryCore)
	if err := upload.RegisterIPFSUploadApp(srv, catalog, cu); err != nil {
		t.Fatalf("upload.RegisterIPFSUploadApp: %v", err)
	}
	if err := RegisterOfficialDescriptor(srv, uploadFileDesc); err != nil {
		t.Fatalf("RegisterOfficialDescriptor(upload_file): %v", err)
	}
	// The model-facing upload_status/list tools (mirrors custom_tools.go's
	// NewAsyncUploadTools registration) share the same manager.
	for _, desc := range upload.NewAsyncUploadTools(mgr) {
		if err := RegisterOfficialDescriptor(srv, desc); err != nil {
			t.Fatalf("RegisterOfficialDescriptor(%s): %v", desc.Name, err)
		}
	}
	if err := RegisterOfficialCuratedTools(srv, catalog); err != nil {
		t.Fatalf("RegisterOfficialCuratedTools: %v", err)
	}
	return srv, cu, mgr
}

// TestIPFSUploadSharedOperation covers the heart of the refactor: the
// model-facing upload_file and the MCP App's ipfs_upload_submit operate on the
// SAME canonical upload handle, so whoever supplies the bytes first becomes the
// authoritative result and no sibling upload is ever created. Concretely it
// proves all five required properties end-to-end over the real MCP surface:
//
//  1. model path can create/use the operation normally (upload_file returns a
//     pre-created canonical handle);
//  2. MCP App path can fulfill that SAME operation (ipfs_upload_submit with
//     the model's handle returns the SAME url+handle — continued, not a mint);
//  3. both paths observe the same final task/CID (upload_status and
//     ipfs_upload_status agree on one completed task/CID);
//  4. a second fulfillment attempt does not create a second upload (re-PUT →
//     404; re-submit → already_claimed; upload_list stays at one task);
//  5. existing non-App/text-only behavior still works (upload_file's Text
//     channel still carries the url + handle for a text-only client).
func TestIPFSUploadSharedOperation(t *testing.T) {
	srv, _, mgr := buildIPFSUploadSharedServer(t)
	cs := connectOfficialClient(t, srv)
	ctx := context.Background()

	// (1) + (5) Model creates/uses the operation; text-only channel carries it.
	modelRes, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "upload_file",
		Arguments: map[string]any{"source": map[string]any{"mode": "mint"}, "name": "shared.bin"},
	})
	if err != nil {
		t.Fatalf("upload_file: %v", err)
	}
	if modelRes.IsError {
		t.Fatalf("upload_file error: %s", requireText(t, modelRes))
	}
	b, _ := json.Marshal(modelRes.StructuredContent)
	var modelSC map[string]any
	_ = json.Unmarshal(b, &modelSC)
	modelURL, _ := modelSC["url"].(string)
	handle, _ := modelSC["upload_handle"].(string)
	if !strings.Contains(modelURL, "/upload/") || handle == "" {
		t.Fatalf("upload_file did not prepare a canonical op: url=%q handle=%q sc=%s", modelURL, handle, b)
	}
	// (5) A text-only MCP client (no widget) still sees the url + handle.
	modelText := requireText(t, modelRes)
	if !strings.Contains(modelText, modelURL) || !strings.Contains(modelText, handle) {
		t.Fatalf("text-only channel missing url/handle: %s", modelText)
	}

	// (2) App continues the SAME operation, not a sibling mint.
	appRes, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "ipfs_upload_submit",
		Arguments: map[string]any{"handle": handle},
	})
	if err != nil {
		t.Fatalf("ipfs_upload_submit: %v", err)
	}
	if appRes.IsError {
		t.Fatalf("ipfs_upload_submit error: %s", requireText(t, appRes))
	}
	b2, _ := json.Marshal(appRes.StructuredContent)
	var appSC map[string]any
	_ = json.Unmarshal(b2, &appSC)
	appURL, _ := appSC["url"].(string)
	if appURL != modelURL {
		t.Fatalf("app continued op returned a DIFFERENT endpoint (sibling):\n  model=%q\n  app  =%q", modelURL, appURL)
	}
	if continued, _ := appSC["continued"].(bool); !continued {
		t.Fatalf("app submit did not report continued=true: %s", b2)
	}
	// The same handle, exactly one pre-created task.
	if appHandle, _ := appSC["upload_handle"].(string); appHandle != handle {
		t.Fatalf("app submit changed handle: want %q got %q", handle, appHandle)
	}
	if req2, _ := mgr.Get(handle); req2 == nil || req2.State != transfer.UploadStatePrepared {
		t.Fatalf("expected exactly one prepared task for the op")
	}
	if n := len(mgr.List()); n != 1 {
		t.Fatalf("expected a single shared operation, got %d tasks", n)
	}

	// App file picker supplies the bytes (the Uppy XHR PUT path).
	req, err := http.NewRequest(http.MethodPut, appURL, strings.NewReader("shared bytes"))
	if err != nil {
		t.Fatalf("PUT req: %v", err)
	}
	putResp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PUT: %v", err)
	}
	defer putResp.Body.Close()
	if putResp.StatusCode != http.StatusAccepted {
		t.Fatalf("PUT status = %d, want 202", putResp.StatusCode)
	}
	var putOut map[string]any
	if err := json.NewDecoder(putResp.Body).Decode(&putOut); err != nil {
		t.Fatalf("decode 202: %v", err)
	}
	if putHandle, _ := putOut["upload_handle"].(string); putHandle != handle {
		t.Fatalf("202 upload_handle = %q, want the same canonical handle %q", putHandle, handle)
	}

	// (3) Both paths observe the same completed task/CID.
	waitCompleted := func(tool, h string) *mcp.CallToolResult {
		t.Helper()
		deadline := time.Now().Add(2 * time.Second)
		for {
			pr, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: tool, Arguments: map[string]any{"handle": h}})
			if err != nil {
				t.Fatalf("%s: %v", tool, err)
			}
			if s, ok := taskStateOf(pr); ok && s == transfer.UploadStateCompleted {
				return pr
			}
			if time.Now().After(deadline) {
				t.Fatalf("%s did not complete in time", tool)
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	modelPoll := waitCompleted("upload_status", handle)
	appPoll := waitCompleted("ipfs_upload_status", handle)

	cidOf := func(r *mcp.CallToolResult) string {
		pb, _ := json.Marshal(r.StructuredContent)
		var m map[string]any
		_ = json.Unmarshal(pb, &m)
		res, _ := m["result"].(map[string]any)
		c, _ := res["cid"].(string)
		return c
	}
	modelCID := cidOf(modelPoll)
	appCID := cidOf(appPoll)
	if modelCID == "" || modelCID != appCID {
		t.Fatalf("model and app observe different CIDs: model=%q app=%q", modelCID, appCID)
	}
	if modelCID != "QmShared" {
		t.Fatalf("unexpected CID %q for shared op", modelCID)
	}

	// (4) A second fulfillment attempt does NOT create a second upload.
	// 4a. Re-PUT to the spent endpoint is rejected (404), never re-accepted.
	reput, err := http.NewRequest(http.MethodPut, appURL, strings.NewReader("second attempt"))
	if err != nil {
		t.Fatalf("re-PUT req: %v", err)
	}
	reputResp, err := http.DefaultClient.Do(reput)
	if err != nil {
		t.Fatalf("re-PUT: %v", err)
	}
	defer reputResp.Body.Close()
	if reputResp.StatusCode != http.StatusNotFound {
		t.Fatalf("re-PUT status = %d, want 404", reputResp.StatusCode)
	}
	// 4b. Re-submit reports already_claimed, not a fresh mint.
	againRes, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "ipfs_upload_submit",
		Arguments: map[string]any{"handle": handle},
	})
	if err != nil {
		t.Fatalf("re-submit: %v", err)
	}
	if againRes.IsError {
		t.Fatalf("re-submit returned error: %s", requireText(t, againRes))
	}
	agb, _ := json.Marshal(againRes.StructuredContent)
	var againSC map[string]any
	_ = json.Unmarshal(agb, &againSC)
	if claimed, _ := againSC["already_claimed"].(bool); !claimed {
		t.Fatalf("re-submit did not report already_claimed: %s", agb)
	}
	// 4c. Still exactly ONE tracked task with the SAME CID — no sibling.
	if n := len(mgr.List()); n != 1 {
		t.Fatalf("second fulfillment created a sibling upload: %d tasks", n)
	}
	finalTask, err := mgr.Get(handle)
	if err != nil {
		t.Fatalf("handle lost after second fulfillment: %v", err)
	}
	if finalTask.Result == nil || finalTask.Result.(map[string]any)["cid"] != modelCID {
		t.Fatalf("final task CID changed after second fulfillment")
	}
}

// TestIPFSUploadSubmitStalePreparedHandle verifies the review fix: a handle
// that was prepared (canonical op minted) but never fulfilled — whose presigned
// endpoint has since lapsed — is reported as STALE (error guiding the app to
// prepare a fresh upload), NOT as already_claimed. A Prepared (byte-less) task
// cannot be polled for a CID, so claiming it would leave the app stuck; and it
// must not be mistaken for a completed/claimed op that the app should just
// poll.
func TestIPFSUploadSubmitStalePreparedHandle(t *testing.T) {
	srv, cu, _ := buildIPFSUploadSharedServer(t)
	cs := connectOfficialClient(t, srv)
	ctx := context.Background()

	// Model prepares a canonical operation (never fulfills it).
	modelRes, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "upload_file",
		Arguments: map[string]any{"source": map[string]any{"mode": "mint"}, "name": "stale.bin"},
	})
	if err != nil {
		t.Fatalf("upload_file: %v", err)
	}
	b, _ := json.Marshal(modelRes.StructuredContent)
	var modelSC map[string]any
	_ = json.Unmarshal(b, &modelSC)
	handle, _ := modelSC["upload_handle"].(string)
	if handle == "" {
		t.Fatalf("upload_file did not return a handle: %s", b)
	}

	// Advance the coordinator's clock past the presigned endpoint's TTL so the
	// handle's endpoint lapses while its task is still Prepared (never
	// fulfilled). The UploadTaskManager keeps real time, so the (recently
	// created) Prepared task is still tracked.
	cu.SetNow(func() time.Time { return time.Now().Add(10 * time.Minute) })

	// The App asks to continue that handle: it must get a STALE error, not an
	// already_claimed result (there are no bytes and no CID to poll).
	staleRes, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "ipfs_upload_submit",
		Arguments: map[string]any{"handle": handle},
	})
	if err != nil {
		t.Fatalf("ipfs_upload_submit: %v", err)
	}
	if !staleRes.IsError {
		t.Fatalf("stale prepared handle must error, got: %s", requireText(t, staleRes))
	}
	if !strings.Contains(requireText(t, staleRes), "prepared but never fulfilled") {
		t.Fatalf("stale error should explain the prepared-but-never-fulfilled state: %s", requireText(t, staleRes))
	}
}

// taskStateOf extracts the task's json "state" field from an SDK poll result.
func taskStateOf(res *mcp.CallToolResult) (transfer.UploadTaskState, bool) {
	b, err := json.Marshal(res.StructuredContent)
	if err != nil {
		return "", false
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return "", false
	}
	s, _ := m["state"].(string)
	return transfer.UploadTaskState(s), s != ""
}

// TestIPFSUploadCORSConfiguredHost verifies a configured MCP-host origin and a
// dynamic, non-loopback sandbox origin are both reflected for the cross-origin
// Uppy PUT. The route is token-gated, so it reflects any origin; a configured
// origin keeps working (and remains valid as part of the LoopbackServer's
// accepted-origin set for other browser consumers).
func TestIPFSUploadCORSConfiguredHost(t *testing.T) {
	srv, cu := buildIPFSUploadAppServer(t)
	// The host serving the ui:// app iframe on its own origin.
	cu.AddTrustedOrigins("https://apps.example.com")
	t.Cleanup(func() { cu.Stop(context.Background()) })
	cs := connectOfficialClient(t, srv)

	mintRes, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "ipfs_upload_submit",
		Arguments: map[string]any{"name": "pic.png"},
	})
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	b, _ := json.Marshal(mintRes.StructuredContent)
	var mintSC map[string]any
	_ = json.Unmarshal(b, &mintSC)
	url, _ := mintSC["url"].(string)

	// Configured host origin: reflected + allowed.
	pre, err := http.NewRequest(http.MethodOptions, url, nil)
	if err != nil {
		t.Fatalf("preflight req: %v", err)
	}
	pre.Header.Set("Origin", "https://apps.example.com")
	pre.Header.Set("Access-Control-Request-Method", "PUT")
	preResp, err := http.DefaultClient.Do(pre)
	if err != nil {
		t.Fatalf("preflight: %v", err)
	}
	preResp.Body.Close()
	if aoc := preResp.Header.Get("Access-Control-Allow-Origin"); aoc != "https://apps.example.com" {
		t.Fatalf("configured host Allow-Origin = %q, want https://apps.example.com", aoc)
	}

	// A dynamic, unlisted sandbox origin is also reflected (token-gated route).
	evil, err := http.NewRequest(http.MethodOptions, url, nil)
	if err != nil {
		t.Fatalf("dynamic-origin preflight req: %v", err)
	}
	evil.Header.Set("Origin", "https://a1b2c3d4e5f6g7h8.host-sandbox.example")
	evil.Header.Set("Access-Control-Request-Method", "PUT")
	evilResp, err := http.DefaultClient.Do(evil)
	if err != nil {
		t.Fatalf("dynamic-origin preflight: %v", err)
	}
	evilResp.Body.Close()
	if aoc := evilResp.Header.Get("Access-Control-Allow-Origin"); aoc != "https://a1b2c3d4e5f6g7h8.host-sandbox.example" {
		t.Fatalf("dynamic origin not reflected: Allow-Origin = %q", aoc)
	}
}

// TestIPFSUploadResourceAdvertisesConnectDomains is the regression test for the
// CSP issue: the upload app does a cross-origin Uppy XHR PUT from the host
// sandbox (e.g. the MCP host's content CDN) to the presigned /upload/<token> endpoint,
// so the app resource MUST advertise that upload origin in its read-level
// _meta.ui.csp.connectDomains or the host sandbox CSP blocks the PUT. The
// origin is resolved dynamically at read time (the tunnel/base URL or loopback
// address is only known after the server is up).
func TestIPFSUploadResourceAdvertisesConnectDomains(t *testing.T) {
	srv, cu := buildIPFSUploadAppServer(t)
	cs := connectOfficialClient(t, srv)

	readConnectDomains := func(t *testing.T) []any {
		t.Helper()
		res, err := cs.ReadResource(context.Background(), &mcp.ReadResourceParams{URI: upload.IPFSUploadAppURI})
		if err != nil {
			t.Fatalf("ReadResource: %v", err)
		}
		if len(res.Contents) == 0 {
			t.Fatalf("read returned no content items")
		}
		// _meta.ui is on the content item, not the result root (ext-apps spec).
		ui, ok := res.Contents[0].Meta["ui"].(map[string]any)
		if !ok {
			t.Fatalf("read content-item _meta.ui missing: %#v", res.Contents[0].Meta)
		}
		csp, ok := ui["csp"].(map[string]any)
		if !ok {
			t.Fatalf("read result _meta.ui.csp missing: %#v", ui)
		}
		cd, ok := csp["connectDomains"].([]any)
		if !ok {
			t.Fatalf("read result _meta.ui.csp.connectDomains missing: %#v", csp)
		}
		if len(cd) == 0 {
			t.Fatalf("connectDomains empty")
		}
		return cd
	}

	// Loopback mode (no base URL): the advertised connect domain is the live
	// loopback origin the Uppy uploader PUTs to.
	if got := readConnectDomains(t)[0]; got != cu.ConnectOrigins()[0] {
		t.Fatalf("loopback connectDomains[0] = %#v, want %q", got, cu.ConnectOrigins()[0])
	}

	// HTTP/tunnel mode: once the coordinator's base URL is resolved (mirroring
	// serveHTTP's SetBaseURL), a subsequent read advertises that origin.
	cu.SetBaseURL("https://tunnel.example.com")
	if got := readConnectDomains(t)[0]; got != "https://tunnel.example.com" {
		t.Fatalf("tunnel connectDomains[0] = %#v, want https://tunnel.example.com", got)
	}
}

// TestOpenUploadManagerStaleHandleFallsBack is a regression test for the
// kody-flagged high-severity bug: passing a non-empty but stale/expired/used
// handle to open_upload_manager must NOT return success with an empty
// presigned_url (which would leave the picker's iframe with no endpoint to PUT
// to). It must fall back to minting a fresh endpoint, report continued=false
// (a brand-new operation, not a continuation), and ALWAYS expose a non-empty
// presigned_url.
func TestOpenUploadManagerStaleHandleFallsBack(t *testing.T) {
	mgr := transfer.NewUploadTaskManager(func(_ context.Context, reader io.Reader, _ int64, name string, _ bool, _ string, _ bool) (any, error) {
		_, _ = io.Copy(io.Discard, reader)
		return map[string]any{"cid": "QmFallback", "name": name}, nil
	}, 0)
	cu := transfer.NewHTTPUpload(mgr, 1<<20)
	t.Cleanup(func() { cu.Stop(context.Background()) })

	desc := upload.NewOpenUploadManagerDescriptor(cu)
	srv := sdk.NewServer(nil)
	if err := RegisterOfficialDescriptor(srv, desc); err != nil {
		t.Fatalf("RegisterOfficialDescriptor(open_upload_manager): %v", err)
	}
	cs := connectOfficialClient(t, srv)
	ctx := context.Background()

	// Valid live handle: continuing succeeds with the SAME endpoint and
	// continued=true.
	validURL, validHandle := cu.Prepare("stale.bin", transfer.DefaultHTTPUploadTTL)
	if validURL == "" || validHandle == "" {
		t.Fatalf("Prepare failed: url=%q handle=%q", validURL, validHandle)
	}
	okRes, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "open_upload_manager",
		Arguments: map[string]any{"handle": validHandle},
	})
	if err != nil {
		t.Fatalf("open_upload_manager(valid handle): %v", err)
	}
	if okRes.IsError {
		t.Fatalf("open_upload_manager(valid handle) error: %s", requireText(t, okRes))
	}
	b, _ := json.Marshal(okRes.StructuredContent)
	var okSC map[string]any
	_ = json.Unmarshal(b, &okSC)
	if okSC["presigned_url"] != validURL {
		t.Fatalf("valid handle: presigned_url = %v, want %q (sc=%s)", okSC["presigned_url"], validURL, b)
	}
	if continued, _ := okSC["continued"].(bool); !continued {
		t.Fatalf("valid handle: expected continued=true, sc=%s", b)
	}

	// Stale/unknown (non-empty) handle: MUST fall back to a fresh mint —
	// non-empty presigned_url, continued=false, and a DIFFERENT endpoint.
	staleRes, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "open_upload_manager",
		Arguments: map[string]any{"handle": "definitely-stale-handle"},
	})
	if err != nil {
		t.Fatalf("open_upload_manager(stale handle): %v", err)
	}
	if staleRes.IsError {
		t.Fatalf("open_upload_manager(stale handle) error: %s", requireText(t, staleRes))
	}
	b2, _ := json.Marshal(staleRes.StructuredContent)
	var staleSC map[string]any
	_ = json.Unmarshal(b2, &staleSC)
	freshURL, _ := staleSC["presigned_url"].(string)
	if freshURL == "" {
		t.Fatalf("stale handle: presigned_url must never be empty (regression), sc=%s", b2)
	}
	if continued, _ := staleSC["continued"].(bool); continued {
		t.Fatalf("stale handle: expected continued=false (fresh op), sc=%s", b2)
	}
	if freshURL == validURL {
		t.Fatalf("stale handle: expected a FRESH endpoint, got the original %q", freshURL)
	}
}
