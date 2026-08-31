//go:build !no_tunnel

package services

import (
	"context"
	"os"
	"testing"

	"go.lumeweb.com/pinner-cli/internal/fieldform"
)

// oauthConfirmPrompter is a fieldform.Prompter that records every Confirm
// default it is asked with and returns a fixed result. It stubs the other
// methods so promptOAuthForInstall (the only code under test here) flows
// through the shared prompt channel bound via fieldform.WithPrompter.
type oauthConfirmPrompter struct {
	confirmCalls []bool
	result       bool
}

func (p *oauthConfirmPrompter) Select(string, []string, string) (int, string, error) {
	return 0, "", nil
}
func (p *oauthConfirmPrompter) MultiSelect(string, []string, []string) ([]string, error) {
	return nil, nil
}
func (p *oauthConfirmPrompter) Confirm(_ string, def bool) (bool, error) {
	p.confirmCalls = append(p.confirmCalls, def)
	return p.result, nil
}
func (p *oauthConfirmPrompter) Text(string, string, string) (string, error) { return "", nil }

// TestPromptOAuthForInstallAsksWithSecureDefault guards that promptOAuthForInstall
// prompts through the shared channel on an undecided install, defaulting to the
// secure default-on (true) and recording the operator's decision into state.
func TestPromptOAuthForInstallAsksWithSecureDefault(t *testing.T) {
	p := &oauthConfirmPrompter{result: true}
	ctx := fieldform.WithPrompter(context.Background(), p)
	s := &ServiceInstallState{}
	if err := promptOAuthForInstall(ctx, s); err != nil {
		t.Fatalf("prompt OAuth failed: %v", err)
	}
	if len(p.confirmCalls) != 1 || p.confirmCalls[0] != true {
		t.Errorf("Confirm defaults = %v, want [true] (secure default-on)", p.confirmCalls)
	}
	if s.OAuth == nil || !*s.OAuth {
		t.Errorf("s.OAuth = %v, want &true from operator decision", s.OAuth)
	}
}

// TestPromptOAuthForInstallHonorsPersistedDefault verifies that a persisted
// MCP_OAUTH=false becomes the prompt's assumed default (still asked, but
// defaulting to the operator's prior opt-out).
func TestPromptOAuthForInstallHonorsPersistedDefault(t *testing.T) {
	p := &oauthConfirmPrompter{result: false}
	ctx := fieldform.WithPrompter(context.Background(), p)
	optOut := false
	s := &ServiceInstallState{OAuth: &optOut}
	if err := promptOAuthForInstall(ctx, s); err != nil {
		t.Fatalf("prompt OAuth failed: %v", err)
	}
	if len(p.confirmCalls) != 1 || p.confirmCalls[0] != false {
		t.Errorf("Confirm defaults = %v, want [false] (persisted opt-out as default)", p.confirmCalls)
	}
	if s.OAuth == nil || *s.OAuth {
		t.Errorf("s.OAuth = %v, want &false", s.OAuth)
	}
}

// TestPromptOAuthForInstallSkips guards the cases where the OAuth prompt must
// NOT ask: a non-interactive run (flag/env-driven) and an explicit --oauth
// switch on the command line (an operator decision that seeds the tri-state).
func TestPromptOAuthForInstallSkips(t *testing.T) {
	// Non-interactive: never prompt.
	prior := fieldform.NonInteractive
	fieldform.NonInteractive = true
	s := &ServiceInstallState{}
	ctx := fieldform.WithPrompter(context.Background(), &oauthConfirmPrompter{})
	if err := promptOAuthForInstall(ctx, s); err != nil {
		t.Fatalf("prompt OAuth failed in non-interactive mode: %v", err)
	}
	if s.OAuth != nil {
		t.Errorf("s.OAuth = %v, want nil (must not prompt in non-interactive mode)", s.OAuth)
	}
	fieldform.NonInteractive = prior

	// A literal --oauth on the command line is an explicit operator decision:
	// do not prompt; leave the tri-state untouched for the flag fold.
	prevArgs := os.Args
	os.Args = append([]string{}, prevArgs...)
	os.Args = append(os.Args, "--oauth=false")
	defer func() { os.Args = prevArgs }()

	p := &oauthConfirmPrompter{result: true}
	s2 := &ServiceInstallState{}
	if err := promptOAuthForInstall(fieldform.WithPrompter(context.Background(), p), s2); err != nil {
		t.Fatalf("prompt OAuth failed with --oauth on the command line: %v", err)
	}
	if s2.OAuth != nil {
		t.Errorf("s.OAuth = %v, want nil (decided by --oauth on the command line)", s2.OAuth)
	}
}

// TestPromptOAuthForInstallEnvSourcedValueDoesNotSuppress is the regression
// guard for the env-sourcing pattern: a persisted/inherited MCP_OAUTH value is
// folded into s.OAuth (a non-nil tri-state) and must become the prompt's
// default, never silently suppress the question.
func TestPromptOAuthForInstallEnvSourcedValueDoesNotSuppress(t *testing.T) {
	p := &oauthConfirmPrompter{result: true}
	ctx := fieldform.WithPrompter(context.Background(), p)
	on := true // folded from MCP_OAUTH=true
	s := &ServiceInstallState{OAuth: &on}
	if err := promptOAuthForInstall(ctx, s); err != nil {
		t.Fatalf("prompt OAuth failed: %v", err)
	}
	if len(p.confirmCalls) != 1 || p.confirmCalls[0] != true {
		t.Errorf("Confirm defaults = %v, want [true] (env value folded as the default)", p.confirmCalls)
	}
}
