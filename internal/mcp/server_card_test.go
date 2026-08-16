package mcp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"go.lumeweb.com/pinner-cli/build"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestServerCardHandler(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/.well-known/mcp/server-card.json", nil)
	serverCardHandler(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "application/json", rec.Header().Get("Content-Type"))

	var card struct {
		ServerInfo struct {
			Name    string `json:"name"`
			Version string `json:"version"`
		} `json:"serverInfo"`
		Authentication struct {
			Required bool     `json:"required"`
			Schemes  []string `json:"schemes"`
		} `json:"authentication"`
		Tools []struct {
			Name        string `json:"name"`
			Description string `json:"description"`
		} `json:"tools"`
		Resources []any `json:"resources"`
		Prompts   []any `json:"prompts"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &card))

	assert.Equal(t, "pinner", card.ServerInfo.Name)
	assert.Equal(t, build.Version, card.ServerInfo.Version)
	assert.True(t, card.Authentication.Required)
	assert.Contains(t, card.Authentication.Schemes, "bearer")
	assert.NotEmpty(t, card.Tools)
	// Primary tool must be present so directories index the core surface.
	assert.True(t, containsTool(card.Tools, "pins_add"))
}

func containsTool(tools []struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}, name string) bool {
	for _, t := range tools {
		if t.Name == name {
			return true
		}
	}
	return false
}

func TestServerCardHandlerRejectsNonProbeMethods(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/.well-known/mcp/server-card.json", nil)
	serverCardHandler(rec, req)
	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)
	assert.Equal(t, "GET, HEAD", rec.Header().Get("Allow"))
}
