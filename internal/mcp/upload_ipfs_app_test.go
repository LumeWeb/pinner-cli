package mcp

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// buildIPFSUploadAppServer constructs the catalog + server the way the adapter
// does: the upload_file descriptor is indexed in the catalog, the app view
// attaches _meta.ui to it, that meta is copied onto the descriptor served
// directly via RegisterOfficialDescriptor (the production surface), and a real
// presigned httpUpload coordinator backs the mint/poll helpers.
func buildIPFSUploadAppServer(t *testing.T) (*mcp.Server, *httpUpload) {
	t.Helper()

	mgr := NewUploadTaskManager(func(_ context.Context, reader io.Reader, _ int64, name string, _ bool) (any, error) {
		_, _ = io.Copy(io.Discard, reader)
		return map[string]any{"cid": "QmApp", "name": name}, nil
	}, 0)
	cu := NewHTTPUpload(mgr, 1<<20)
	t.Cleanup(func() { cu.Stop(context.Background()) })

	catalog := NewToolCatalog()
	srv := NewOfficialServer(nil)

	// Mirror the production registration sequence in registerCustomTools.
	uploadFileDesc := ToolDescriptor{
		Name:        "upload_file",
		Title:       "Upload to IPFS",
		Description: "Upload a file to Pinner over IPFS.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{}}`),
		Handler: func(_ context.Context, _ ToolRequest) (ToolResult, error) {
			return ToolResult{Text: "ok"}, nil
		},
	}
	catalog.Add(toolEntryFromDescriptor(uploadFileDesc))
	if err := RegisterIPFSUploadApp(srv, catalog, cu); err != nil {
		t.Fatalf("RegisterIPFSUploadApp: %v", err)
	}
	// Production copies the app-view _meta from the catalog entry onto the
	// descriptor served directly so the direct tools/list surface sees it.
	if entry, ok := catalog.Get("upload_file"); ok {
		if uploadFileDesc.Meta == nil {
			uploadFileDesc.Meta = map[string]any{}
		}
		for k, v := range entry.Meta {
			uploadFileDesc.Meta[k] = v
		}
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
		if r.URI == IPFSUploadAppURI {
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
	var upTool, mintTool, pollTool *mcp.Tool
	for _, x := range tres.Tools {
		switch x.Name {
		case "upload_file":
			upTool = x
		case "ipfs_upload_submit":
			mintTool = x
		case "ipfs_upload_status":
			pollTool = x
		}
	}
	if upTool == nil {
		t.Fatalf("upload_file not listed")
	}
	ui, ok := upTool.Meta["ui"].(map[string]any)
	if !ok {
		t.Fatalf("_meta.ui missing on upload_file: %T", upTool.Meta["ui"])
	}
	if got := ui["resourceUri"]; got != IPFSUploadAppURI {
		t.Fatalf("_meta.ui.resourceUri = %#v, want %q", got, IPFSUploadAppURI)
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
		if s, ok := taskStateOf(pollRes); ok && s == UploadStateCompleted {
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
	srv := NewOfficialServer(nil)
	catalog := NewToolCatalog()
	if err := RegisterIPFSUploadApp(srv, catalog, nil); err == nil {
		t.Fatalf("RegisterIPFSUploadApp with nil coordinator must fail")
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

	// Untrusted-origin preflight: the response must NOT grant CORS, so the
	// browser refuses the cross-origin write despite knowing the token.
	evil, err := http.NewRequest(http.MethodOptions, urlVal, nil)
	if err != nil {
		t.Fatalf("evil preflight req: %v", err)
	}
	evil.Header.Set("Origin", "https://evil.example.com")
	evil.Header.Set("Access-Control-Request-Method", "PUT")
	evilResp, err := http.DefaultClient.Do(evil)
	if err != nil {
		t.Fatalf("evil preflight: %v", err)
	}
	evilResp.Body.Close()
	if aoc := evilResp.Header.Get("Access-Control-Allow-Origin"); aoc != "" {
		t.Fatalf("untrusted origin allowed: Allow-Origin = %q, want empty", aoc)
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

// taskStateOf extracts the task's json "state" field from an SDK poll result.
func taskStateOf(res *mcp.CallToolResult) (UploadTaskState, bool) {
	b, err := json.Marshal(res.StructuredContent)
	if err != nil {
		return "", false
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return "", false
	}
	s, _ := m["state"].(string)
	return UploadTaskState(s), s != ""
}

// TestIPFSUploadCORSConfiguredHost verifies a configured MCP-host origin
// (added via AddTrustedOrigins) is reflected for the cross-origin Uppy PUT,
// while an arbitrary unlisted origin is still refused — the configurable
// allowlist keeps a far host working without opening the endpoint to any page.
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

	// Arbitrary unlisted origin: still refused.
	evil, err := http.NewRequest(http.MethodOptions, url, nil)
	if err != nil {
		t.Fatalf("evil preflight req: %v", err)
	}
	evil.Header.Set("Origin", "https://evil.example.com")
	evil.Header.Set("Access-Control-Request-Method", "PUT")
	evilResp, err := http.DefaultClient.Do(evil)
	if err != nil {
		t.Fatalf("evil preflight: %v", err)
	}
	evilResp.Body.Close()
	if aoc := evilResp.Header.Get("Access-Control-Allow-Origin"); aoc != "" {
		t.Fatalf("unlisted origin allowed: Allow-Origin = %q, want empty", aoc)
	}
}
