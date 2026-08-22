package sdk

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"go.lumeweb.com/pinner-cli/internal/mcp/core/model"
)

// sdkTestClient wires an in-memory client session to a server for exercising
// resources/read and resources/list through the public protocol surface.
func sdkTestClient(t *testing.T, srv *Server) *mcp.ClientSession {
	t.Helper()
	ctx := context.Background()
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	if _, err := srv.Connect(ctx, serverTransport, nil); err != nil {
		t.Fatalf("server connect: %v", err)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "v0.0.1"}, nil)
	cs, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return cs
}

// uiMetaOf extracts the `ui` map from a resource's _meta, or nil when absent.
func uiMetaOf(t *testing.T, meta mcp.Meta) map[string]any {
	t.Helper()
	ui, ok := meta["ui"].(map[string]any)
	if !ok {
		return nil
	}
	return ui
}

// connectDomainsOf returns the decoded csp.connectDomains from a `ui` meta map.
func connectDomainsOf(t *testing.T, ui map[string]any) []string {
	t.Helper()
	if ui == nil {
		return nil
	}
	cspAny, ok := ui["csp"].(map[string]any)
	if !ok {
		return nil
	}
	raw, err := json.Marshal(cspAny["connectDomains"])
	if err != nil {
		t.Fatalf("marshal connectDomains: %v", err)
	}
	var out []string
	if len(raw) > 0 && string(raw) != "null" {
		if err := json.Unmarshal(raw, &out); err != nil {
			t.Fatalf("unmarshal connectDomains: %v", err)
		}
	}
	return out
}

func TestSetAppResourceConnectDomainsBakesListEntry(t *testing.T) {
	srv := NewServer(nil)
	const uri = "ui://uploads/ipfs.html"
	pref := true
	res := AppResource{
		URI:         uri,
		Name:        "Upload to IPFS",
		Title:       "Upload to IPFS",
		Description: "app",
		HTML:        "<html></html>",
		Meta: model.AppResourceMeta{
			Domain:        "uploads.ipfs",
			PrefersBorder: &pref,
		},
		// Mirrors production: the upload apps resolve the live origin at read
		// time via ConnectDomainsFunc (the tunnel/base URL).
		ConnectDomainsFunc: func() []string {
			return []string{"https://tunnel.ngrok-free.dev"}
		},
	}
	if err := RegisterAppResource(srv, res); err != nil {
		t.Fatalf("register: %v", err)
	}

	// Before the live origin is resolved the list entry must NOT advertise any
	// connectDomains (the registration-time static fallback), matching ext-apps'
	// "static default reviewed at connection time" semantics.
	cs := sdkTestClient(t, srv)
	listBefore, err := cs.ListResources(context.Background(), nil)
	if err != nil {
		t.Fatalf("list (before): %v", err)
	}
	foundBefore := false
	for _, r := range listBefore.Resources {
		if r.URI != uri {
			continue
		}
		foundBefore = true
		if got := connectDomainsOf(t, uiMetaOf(t, r.Meta)); len(got) != 0 {
			t.Fatalf("list entry advertised connectDomains before resolve: %v", got)
		}
	}
	if !foundBefore {
		t.Fatal("resource not present in list before resolve")
	}

	// Simulate the transport resolving its origin (e.g. the tunnel URL) after
	// registration, then confirm the list entry now advertises it while the
	// static siblings survive.
	if err := SetAppResourceConnectDomains(srv, uri, []string{"https://tunnel.ngrok-free.dev"}); err != nil {
		t.Fatalf("set connect domains: %v", err)
	}
	listAfter, err := cs.ListResources(context.Background(), nil)
	if err != nil {
		t.Fatalf("list (after): %v", err)
	}
	var listMeta map[string]any
	for _, r := range listAfter.Resources {
		if r.URI == uri {
			listMeta = uiMetaOf(t, r.Meta)
			break
		}
	}
	if listMeta == nil {
		t.Fatal("resource missing from list after resolve")
	}
	if got := connectDomainsOf(t, listMeta); len(got) != 1 || got[0] != "https://tunnel.ngrok-free.dev" {
		t.Fatalf("list connectDomains = %v, want tunnel origin", got)
	}
	// Static siblings must be preserved across re-registration.
	if d, _ := listMeta["domain"].(string); d != "uploads.ipfs" {
		t.Fatalf("domain lost after re-register: %v", d)
	}
	if p, _ := listMeta["prefersBorder"].(bool); !p {
		t.Fatalf("prefersBorder lost after re-register: %v", p)
	}

	// The read result still serves the app and must carry the same origin.
	rr, err := cs.ReadResource(context.Background(), &mcp.ReadResourceParams{URI: uri})
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if got := connectDomainsOf(t, uiMetaOf(t, rr.Meta)); len(got) != 1 || got[0] != "https://tunnel.ngrok-free.dev" {
		t.Fatalf("read connectDomains = %v, want tunnel origin", got)
	}
}

func TestSetAppResourceConnectDomainsUnknownURI(t *testing.T) {
	srv := NewServer(nil)
	if err := SetAppResourceConnectDomains(srv, "ui://does/not-exist.html", []string{"https://x.dev"}); err == nil {
		t.Fatal("expected error for unregistered uri")
	}
}

// registerSameURITwoServers asserts that per-server state is keyed by server:
// two servers in one process registering the SAME app URI must not collide, so
// SetAppResourceConnectDomains on one never mutates the other's list entry.
func TestSetAppResourceConnectDomainsIsolationPerServer(t *testing.T) {
	const uri = "ui://uploads/ipfs.html"
	register := func(t *testing.T, id string) *Server {
		srv := NewServer(nil)
		if err := RegisterAppResource(srv, AppResource{
			URI: uri, Name: id, Title: id, Description: id, HTML: "<html></html>",
			ConnectDomainsFunc: func() []string { return []string{"https://" + id + ".dev"} },
		}); err != nil {
			t.Fatalf("register on %s: %v", id, err)
		}
		return srv
	}

	srvA := register(t, "a")
	srvB := register(t, "b")

	// Baking connectDomains onto srvA must NOT leak into srvB's registry entry,
	// and both registrations must coexist in the per-server map.
	if err := SetAppResourceConnectDomains(srvA, uri, []string{"https://tunnel-a.ngrok-free.dev"}); err != nil {
		t.Fatalf("set connect domains on srvA: %v", err)
	}

	// srvB can still be updated independently (its retained meta/handler survived
	// the srvA update), proving no cross-server overwrite.
	if err := SetAppResourceConnectDomains(srvB, uri, []string{"https://tunnel-b.ngrok-free.dev"}); err != nil {
		t.Fatalf("set connect domains on srvB: %v", err)
	}

	findList := func(t *testing.T, cs *mcp.ClientSession) map[string]any {
		t.Helper()
		res, err := cs.ListResources(context.Background(), nil)
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		for _, r := range res.Resources {
			if r.URI == uri {
				return uiMetaOf(t, r.Meta)
			}
		}
		t.Fatal("resource missing from list")
		return nil
	}

	gotA := connectDomainsOf(t, findList(t, sdkTestClient(t, srvA)))
	gotB := connectDomainsOf(t, findList(t, sdkTestClient(t, srvB)))
	if len(gotA) != 1 || gotA[0] != "https://tunnel-a.ngrok-free.dev" {
		t.Fatalf("srvA connectDomains = %v, want tunnel-a", gotA)
	}
	if len(gotB) != 1 || gotB[0] != "https://tunnel-b.ngrok-free.dev" {
		t.Fatalf("srvB connectDomains = %v, want tunnel-b", gotB)
	}
}
