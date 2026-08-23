package mcp

import (
	"bytes"
	"embed"
	"text/template"
)

// Prompt template files live as real .tmpl files under prompttemplates/ so the
// instructional prose the agent receives stays editable as files (not as
// giant Go string constants) and are embedded via promptTemplatesFS. Each
// prompt handler renders its messages from named {{define}} blocks below.

//go:embed prompttemplates
var promptTemplatesFS embed.FS

// sitePromptData carries the optional values templated into the
// website-onboarding and website-update prompts. Empty fields select the
// "ask the user" variant of a step instead of the pre-filled step.
type sitePromptData struct {
	Domain        string
	ContentSource string
	TargetType    string
	DNSMode       string

	// website-update fields.
	WebsiteArg  string
	CID         string
	CurrentType string
}

// renderPromptTemplate renders the named prompt template (a
// prompttemplates/*.tmpl {{define}} block) with the given data into a string.
//
// The templates are embedded and static, so parsing is a build-time invariant;
// a failure is a programming error and panics (mirroring mcpAppModule and the
// embedded-client handling). text/template (not html/template) is used so the
// instructional prose is emitted verbatim with no HTML escaping.
func renderPromptTemplate(name string, data sitePromptData) string {
	tpl, err := template.ParseFS(promptTemplatesFS, "prompttemplates/*.tmpl")
	if err != nil {
		panic("mcp: parse prompt templates: " + err.Error())
	}
	var b bytes.Buffer
	if err := tpl.ExecuteTemplate(&b, name, data); err != nil {
		panic("mcp: render prompt template: " + err.Error())
	}
	return b.String()
}
