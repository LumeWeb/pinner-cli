package cli

import (
	"github.com/urfave/cli/v3"
)

const (
	// DocumentationURL is the URL for the Pinner CLI documentation.
	DocumentationURL = "https://docs.pinner.xyz"

	// TutorialCID is a sample v1 CID for use in tutorial examples.
	TutorialCID = "bafybeie7m2fsbt6sjtn7tymyb6sim7iiyz6szl4ethtn7anzx4frzfzipu"
)

// TutorialMetadata contains information for displaying a command in the tutorial.
type TutorialMetadata struct {
	Priority    int    // Order in tutorial (1=first, higher values come later)
	Description string // Brief description for tutorial (uses Description if empty)
	Example     string // Specific usage example (uses Usage if empty)
}

// TutorialCommand wraps a cli.Command with tutorial metadata.
type TutorialCommand struct {
	*cli.Command
	Metadata *TutorialMetadata
}

// TutorialCommands returns the list of commands to display in the tutorial,
// extracted dynamically from the root command's subcommands.
func TutorialCommands(rootCmd *cli.Command) []TutorialCommand {
	var tutorialCmds []TutorialCommand

	for _, subcmd := range rootCmd.Commands {
		if metaValue, ok := subcmd.Metadata["tutorial"]; ok {
			if meta, ok := metaValue.(*TutorialMetadata); ok && meta != nil {
				tutorialCmds = append(tutorialCmds, TutorialCommand{
					Command:  subcmd,
					Metadata: meta,
				})
			}
		}
	}

	// Sort by priority
	sortTutorialCommands(tutorialCmds)

	return tutorialCmds
}

// sortTutorialCommands sorts tutorial commands by priority.
func sortTutorialCommands(cmds []TutorialCommand) {
	for i := 0; i < len(cmds)-1; i++ {
		for j := i + 1; j < len(cmds); j++ {
			if cmds[i].Metadata.Priority > cmds[j].Metadata.Priority {
				cmds[i], cmds[j] = cmds[j], cmds[i]
			}
		}
	}
}

// BuildTutorialCommandsTable returns headers and rows for displaying tutorial commands.
func BuildTutorialCommandsTable(rootCmd *cli.Command) ([]string, [][]string) {
	commands := TutorialCommands(rootCmd)

	headers := []string{"Command", "Usage", "Description"}
	rows := make([][]string, len(commands))
	for i, tc := range commands {
		desc := tc.Metadata.Description
		if desc == "" {
			desc = tc.Description
		}
		rows[i] = []string{tc.Name, tc.Usage, desc}
	}

	return headers, rows
}

// BuildTutorialExamplesTable returns headers and rows for displaying tutorial examples.
func BuildTutorialExamplesTable(rootCmd *cli.Command) ([]string, [][]string) {
	commands := TutorialCommands(rootCmd)

	headers := []string{"Example"}
	rows := make([][]string, len(commands))
	for i, tc := range commands {
		example := tc.Metadata.Example
		if example == "" {
			example = "pinner " + tc.Name
		}
		rows[i] = []string{example}
	}

	return headers, rows
}

// WithTutorial returns a metadata map with tutorial information for a command.
// This helper simplifies adding tutorial metadata to cli.Command definitions.
//
// Example usage:
//
//	Metadata: WithTutorial(1, "Upload and pin a file", "pinner upload myfile.txt")
func WithTutorial(priority int, description, example string) map[string]interface{} {
	return map[string]interface{}{
		"tutorial": &TutorialMetadata{
			Priority:    priority,
			Description: description,
			Example:     example,
		},
	}
}

// abbreviateCID creates the "... style" for a CID (e.g., "bafybeie7m...").
// If the CID is 10 characters or less, returns it unchanged.
func abbreviateCID(cidStr string) string {
	if len(cidStr) <= 10 {
		return cidStr
	}
	return cidStr[:10] + "..."
}
