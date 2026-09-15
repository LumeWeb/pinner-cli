package cli

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	ipfs "go.lumeweb.com/ipfs-sdk"
	"go.lumeweb.com/pinner"
	"go.lumeweb.com/pinner/core/workspaces"
)

// workspaces_label.go maps the workspace's opaque URL label (its user-facing
// identity) onto its sequential numeric ID. Generated labels settled on the
// bare slug (portal-plugin-ipfs#1040, e.g. "ugki684o" -> <slug>.build.<root>);
// legacy labels carry a "ws-" prefix (e.g. "ws-ugki684o"). The strip-fallback
// logic here accepts both, so present-day and post-#1040 workspaces answer to
// the same short form.

// workspaceLabelLegacyPrefix is the prefix stripped from legacy generated
// workspace labels ("ws-<slug>") to produce the short user-facing ID.
const workspaceLabelLegacyPrefix = "ws-"

// workspaceSlugFromLabel is the user-facing workspace ID implied by a label:
// the label with any legacy "ws-" prefix removed.
func workspaceSlugFromLabel(label string) string {
	return strings.TrimPrefix(label, workspaceLabelLegacyPrefix)
}

// workspaceSlugID is the user-facing ID for a workspace response: the slug
// form of its label, falling back to the numeric ID when the label is empty.
func workspaceSlugID(w *ipfs.WorkspaceResponse) string {
	if w == nil {
		return ""
	}
	if w.Label == "" {
		return strconv.Itoa(w.Id)
	}
	return workspaceSlugFromLabel(w.Label)
}

// workspaceScanPageSize is the page size used when scanning the user's
// workspace list to map a label onto its numeric ID.
const workspaceScanPageSize = 100

// labelResolvingWorkspaces decorates a workspaces.Service so the
// single-workspace operations (get/attach/suspend/resume/access/delete)
// accept the workspace's label slug (or its full legacy "ws-<slug>" label) as
// the `id` argument, resolving it to the numeric ID by scanning the paged
// list. A numeric id is treated as the numeric row id without a scan — that
// keeps the numeric path scan-free and gives a deterministic winner when the
// id is ambiguous between a row id and a purely numeric label slug (the
// numeric interpretation takes precedence). Non-numeric slugs that match
// nothing fail explicitly rather than letting the backend 404 them.
//
// Both CLI and MCP surfaces build their workspaces catalog deps through this
// wrapper, so label-based control is consistent across frontends.
type labelResolvingWorkspaces struct {
	inner workspaces.Service
}

// wrapLabelResolvingWorkspaces returns svc wrapped by labelResolvingWorkspaces
// (a nil inner is returned unchanged; catalogops rejects it downstream).
func wrapLabelResolvingWorkspaces(svc workspaces.Service) workspaces.Service {
	if svc == nil {
		return nil
	}
	return &labelResolvingWorkspaces{inner: svc}
}

func (s *labelResolvingWorkspaces) RequireAuthenticated() error {
	return s.inner.RequireAuthenticated()
}

func (s *labelResolvingWorkspaces) SetAuthToken(token string) {
	s.inner.SetAuthToken(token)
}

func (s *labelResolvingWorkspaces) List(ctx context.Context, opts workspaces.ListOptions) ([]ipfs.WorkspaceResponse, error) {
	return s.inner.List(ctx, opts)
}

func (s *labelResolvingWorkspaces) Create(ctx context.Context, req ipfs.WorkspaceRequest) (*ipfs.WorkspaceResponse, error) {
	return s.inner.Create(ctx, req)
}

func (s *labelResolvingWorkspaces) Get(ctx context.Context, id string) (*ipfs.WorkspaceResponse, error) {
	resolved, err := s.resolveID(ctx, id)
	if err != nil {
		return nil, err
	}
	return s.inner.Get(ctx, resolved)
}

func (s *labelResolvingWorkspaces) Delete(ctx context.Context, id string) (*ipfs.WorkspaceResponse, error) {
	resolved, err := s.resolveID(ctx, id)
	if err != nil {
		return nil, err
	}
	return s.inner.Delete(ctx, resolved)
}

func (s *labelResolvingWorkspaces) Access(ctx context.Context, id string, rotate bool) (*ipfs.WorkspaceAccessResponse, error) {
	resolved, err := s.resolveID(ctx, id)
	if err != nil {
		return nil, err
	}
	return s.inner.Access(ctx, resolved, rotate)
}

func (s *labelResolvingWorkspaces) Attach(ctx context.Context, id string, websiteID int) (*ipfs.WorkspaceResponse, error) {
	resolved, err := s.resolveID(ctx, id)
	if err != nil {
		return nil, err
	}
	return s.inner.Attach(ctx, resolved, websiteID)
}

func (s *labelResolvingWorkspaces) Resume(ctx context.Context, id string) (*ipfs.WorkspaceResponse, error) {
	resolved, err := s.resolveID(ctx, id)
	if err != nil {
		return nil, err
	}
	return s.inner.Resume(ctx, resolved)
}

func (s *labelResolvingWorkspaces) Suspend(ctx context.Context, id string) (*ipfs.WorkspaceResponse, error) {
	resolved, err := s.resolveID(ctx, id)
	if err != nil {
		return nil, err
	}
	return s.inner.Suspend(ctx, resolved)
}

// resolveID maps a workspace id argument to the numeric ID the backend
// expects. Numeric ids are the numeric row id, taken as-is; non-numeric ids
// match the workspace's label slug (tolerating the legacy "ws-<slug>" form
// and the full label) by scanning the user's paged list.
func (s *labelResolvingWorkspaces) resolveID(ctx context.Context, id string) (string, error) {
	if id == "" {
		return id, nil
	}
	if _, err := strconv.Atoi(id); err == nil {
		return id, nil
	}
	matchesLabel := func(w ipfs.WorkspaceResponse) (bool, error) {
		return workspaceSlugFromLabel(w.Label) == id || w.Label == id, nil
	}
	ws, found, err := pinner.ScanPagesWithOptions(ctx, s, matchesLabel, nil, workspaceScanPageSize)
	if err != nil {
		return "", err
	}
	if !found {
		return "", fmt.Errorf("workspace %q not found", id)
	}
	return strconv.Itoa(ws.Id), nil
}
