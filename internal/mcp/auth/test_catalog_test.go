package auth

import "go.lumeweb.com/pinner-cli/internal/mcp/core/model"

// testCatalog is a minimal in-memory catalog double for app-view registration
// tests. It satisfies the apps.AppCatalog surface the auth app views require
// (Add + Get) so hub-side tests can register apps without depending on the hub
// ToolCatalog's full surface.
type testCatalog struct {
	entries map[string]*model.ToolEntry
}

func newTestCatalog() *testCatalog {
	return &testCatalog{entries: map[string]*model.ToolEntry{}}
}

func (c *testCatalog) Add(entry *model.ToolEntry) {
	if c.entries == nil {
		c.entries = map[string]*model.ToolEntry{}
	}
	c.entries[entry.Name] = entry
}

func (c *testCatalog) Get(name string) (*model.ToolEntry, bool) {
	e, ok := c.entries[name]
	return e, ok
}
