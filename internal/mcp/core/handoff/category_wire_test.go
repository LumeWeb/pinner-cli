package handoff

import (
	"testing"

	"github.com/stretchr/testify/require"
	"go.lumeweb.com/mcpplane/model"
)

// TestResumeToolSpecCategoryWireValues pins the WIRE values of the tool
// categories the resume-tool seam emits. The category vocabulary comes from
// the shared mcpplane/model constants, where the vault family's wire value is
// "storage" (see mcpplane/model.ToolCategory's documented mapping). This test
// keeps that wire-visible value explicit — never silent — so anyone touching the
// category values sees the intended strings.
func TestResumeToolSpecCategoryWireValues(t *testing.T) {
	require.Equal(t, model.ToolCategory("storage"), model.CategoryStorage,
		"the storage family (vault flows) MUST emit the shared-vocabulary wire value \"storage\" on tools/list")
	require.Equal(t, model.ToolCategory("account"), model.CategoryAccount)
	require.Equal(t, model.ToolCategory("core"), model.CategoryCore)

	// The spec passes an explicit category through unchanged and defaults the
	// zero value to core (computed at descriptor build; the handoff tool
	// rendering path consumes CategoryOrDefault).
	spec := ResumeToolSpec{Name: "x", Category: model.CategoryStorage}
	require.Equal(t, model.CategoryStorage, spec.CategoryOrDefault())
	require.Equal(t, model.CategoryCore, ResumeToolSpec{Name: "x"}.CategoryOrDefault())
}
