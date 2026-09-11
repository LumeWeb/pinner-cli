package clicatalog

import (
	"testing"

	opmesh "go.lumeweb.com/opmesh"
)

// TestAgentRequiredNotRequiredCLIFlag pins the CLI half of the
// TestAgentRequiredMcpOnly contract (its MCP-side half lives in the
// go.lumeweb.com/pinner module's compile_mcp_test.go): an arg marked
// AgentRequired is required of an agent on the MCP surface but is NEVER a
// required urfave CLI flag, so a positionally-supplied CLI value keeps working.
func TestAgentRequiredNotRequiredCLIFlag(t *testing.T) {
	c := opmesh.NewCatalog()
	if err := c.Add(opmesh.NewOperation(opmesh.OperationSpec{
		Name: "job.submit", Title: "Submit", Summary: "submit a job",
		Category: "job", Safety: opmesh.SafetyMutate,
		Interaction: opmesh.InteractionAgentSafe, Visibility: opmesh.VisibilityModel,
		// cids is a StringSlice accepted positionally on the CLI, but the MCP
		// surface requires it.
		Args: []opmesh.OperationArg{
			{Name: "cids", Type: opmesh.ArgTypeStringSlice, AgentRequired: true},
		},
		Handler: &captureHandler{},
	})); err != nil {
		t.Fatalf("Add: %v", err)
	}

	cmd, err := NewCLILeaf(findOp(t, c, "job.submit"), "job.submit", "", nil)
	if err != nil {
		t.Fatalf("NewCLILeaf: %v", err)
	}
	for _, f := range cmd.Flags {
		if f.Names()[0] == "cids" {
			if r, ok := f.(interface{ IsRequired() bool }); ok && r.IsRequired() {
				t.Error("AgentRequired arg cids must NOT be a required CLI flag")
			}
		}
	}
}
