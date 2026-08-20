package fieldform

// NonInteractive disables all interactive prompts in the field-form framework.
// Set it before running a wizard to force a headless/flag-driven run (the
// parent cli package sets it from the install command's headless mode, and the
// MCP form runtime keeps it false). Gather reads it to decide whether an
// unresolved required field is a hard error (headless) or an interactive
// prompt.
var NonInteractive bool
