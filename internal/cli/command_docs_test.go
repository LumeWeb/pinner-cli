package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v3"
)

func TestTutorialCommandsEmpty(t *testing.T) {
	root := &cli.Command{}
	cmds := TutorialCommands(root)
	assert.Empty(t, cmds)
}

func TestTutorialCommandsFromRoot(t *testing.T) {
	root := &cli.Command{
		Commands: []*cli.Command{
			{Name: "upload", Metadata: WithTutorial(2, "Upload a file", "pinner upload file.txt")},
			{Name: "auth", Metadata: WithTutorial(1, "Authenticate", "pinner auth")},
			{Name: "list", Metadata: WithTutorial(3, "List pins", "pinner list")},
			{Name: "config"},
		},
	}

	cmds := TutorialCommands(root)
	require.Len(t, cmds, 3)
	assert.Equal(t, "auth", cmds[0].Name)
	assert.Equal(t, "upload", cmds[1].Name)
	assert.Equal(t, "list", cmds[2].Name)
}

func TestBuildTutorialCommandsTable(t *testing.T) {
	root := &cli.Command{
		Commands: []*cli.Command{
			{Name: "upload", Usage: "Upload files", Description: "Upload desc", Metadata: WithTutorial(1, "Upload a file", "pinner upload file.txt")},
		},
	}

	headers, rows := BuildTutorialCommandsTable(root)
	assert.Equal(t, []string{"Command", "Usage", "Description"}, headers)
	require.Len(t, rows, 1)
	assert.Equal(t, "upload", rows[0][0])
	assert.Equal(t, "Upload a file", rows[0][2])
}

func TestBuildTutorialCommandsTableFallbackDescription(t *testing.T) {
	root := &cli.Command{
		Commands: []*cli.Command{
			{Name: "upload", Usage: "Upload files", Description: "Fallback desc", Metadata: WithTutorial(1, "", "pinner upload file.txt")},
		},
	}

	_, rows := BuildTutorialCommandsTable(root)
	require.Len(t, rows, 1)
	assert.Equal(t, "Fallback desc", rows[0][2])
}

func TestBuildTutorialExamplesTable(t *testing.T) {
	root := &cli.Command{
		Commands: []*cli.Command{
			{Name: "upload", Metadata: WithTutorial(1, "Upload a file", "pinner upload myfile.txt")},
		},
	}

	headers, rows := BuildTutorialExamplesTable(root)
	assert.Equal(t, []string{"Example"}, headers)
	require.Len(t, rows, 1)
	assert.Equal(t, "pinner upload myfile.txt", rows[0][0])
}

func TestBuildTutorialExamplesTableFallback(t *testing.T) {
	root := &cli.Command{
		Commands: []*cli.Command{
			{Name: "upload", Metadata: WithTutorial(1, "Upload a file", "")},
		},
	}

	_, rows := BuildTutorialExamplesTable(root)
	require.Len(t, rows, 1)
	assert.Equal(t, "pinner upload", rows[0][0])
}

func TestWithTutorial(t *testing.T) {
	meta := WithTutorial(1, "desc", "example")
	require.Contains(t, meta, "tutorial")
	tm, ok := meta["tutorial"].(*TutorialMetadata)
	require.True(t, ok)
	assert.Equal(t, 1, tm.Priority)
	assert.Equal(t, "desc", tm.Description)
	assert.Equal(t, "example", tm.Example)
}

func TestAbbreviateCID(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"short CID", "bafybeie7", "bafybeie7"},
		{"long CID", "bafybeie7m2fsbt6sjtn7tymyb6sim7iiyz6szl4ethtn7anzx4frzfzipu", "bafybeie7m..."},
		{"empty", "", ""},
		{"exactly 10", "1234567890", "1234567890"},
		{"11 chars", "12345678901", "1234567890..."},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, abbreviateCID(tt.input))
		})
	}
}

func TestDocumentationURL(t *testing.T) {
	assert.Equal(t, "https://docs.pinner.xyz", DocumentationURL)
}

func TestTutorialCID(t *testing.T) {
	assert.NotEmpty(t, TutorialCID)
}
