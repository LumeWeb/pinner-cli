package catalog

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// newSelectorOp builds an operation with a SelectionGroup over a StringSlice
// ("cids") and a Bool ("all", default "false"), mirroring pins_rm's remove
// selector, plus an unrelated required arg so required-ness doesn't mask the
// selector gate.
func newSelectorOp(h *captureHandler) *catalogImpl {
	c := NewCatalog()
	if err := c.Add(NewOperation(OperationSpec{
		Name: "pin.remove", Title: "Remove", Summary: "remove",
		Category: "pin", Safety: SafetyMutate,
		Interaction: InteractionAgentSafe, Visibility: VisibilityModel,
		Args: []OperationArg{
			{Name: "cids", Type: ArgTypeStringSlice, SelectionGroup: "remove"},
			{Name: "all", Type: ArgTypeBool, SelectionGroup: "remove", Default: "false"},
		},
		Handler: h,
	})); err != nil {
		panic(err)
	}
	return c.(*catalogImpl)
}

// TestSelectorGroupRejectsAmbiguity verifies the centralized gate rejects the
// destructive-ambiguity direction: a SelectionGroup with MORE than one selected
// member. Bool false (from default fill or explicit) is never a selection; an
// empty slice is never a selection. A zero-selected group is intentionally not
// rejected here (the operation's handler owns the empty/incomplete case with a
// descriptive message).
func TestSelectorGroupRejectsAmbiguity(t *testing.T) {
	tests := []struct {
		name  string
		input map[string]any
		// wantErr true => ErrSelector expected (ambiguity); false => passes gate.
		wantErr bool
	}{
		{"both selected", map[string]any{"cids": []string{"x"}, "all": true}, true},
		{"cids + all true", map[string]any{"cids": []string{"a", "b"}, "all": true}, true},
		{"none selected falls through", map[string]any{}, false},
		{"empty slice + all absent is none", map[string]any{"cids": []string{}}, false},
		{"empty slice + all false is none", map[string]any{"cids": []string{}, "all": false}, false},
		{"cids selected", map[string]any{"cids": []string{"x"}}, false},
		{"all selected", map[string]any{"all": true}, false},
		{"cids + all default false", map[string]any{"cids": []string{"x"}, "all": false}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := &captureHandler{}
			c := newSelectorOp(h)
			_, err := c.Invoke(context.Background(), "pin.remove", tt.input, ActorModel)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected ErrSelector, got nil")
				}
				if !errors.Is(err, ErrSelector) {
					t.Fatalf("expected ErrSelector, got %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("expected gate to pass, got %v", err)
			}
		})
	}
}

// TestSelectorGroupRejectsNumericZero verifies a default-filled zero (or an
// explicitly passed zero) does not count as a selected member for a numeric
// selector member, consistent with the bool/string/slice empty semantics.
func TestSelectorGroupRejectsNumericZero(t *testing.T) {
	h := &captureHandler{}
	c := NewCatalog()
	if err := c.Add(NewOperation(OperationSpec{
		Name: "net.bind", Title: "Bind", Summary: "bind",
		Category: "net", Safety: SafetyMutate,
		Interaction: InteractionAgentSafe, Visibility: VisibilityModel,
		Args: []OperationArg{
			{Name: "port", Type: ArgTypeInt, SelectionGroup: "endpoint", Default: "0"},
			{Name: "sock", Type: ArgTypeString, SelectionGroup: "endpoint"},
		},
		Handler: h,
	})); err != nil {
		t.Fatalf("Add: %v", err)
	}
	// port default-filled to 0 must NOT count as selected; sock absent -> pass.
	if _, err := c.Invoke(context.Background(), "net.bind", map[string]any{}, ActorModel); err != nil {
		t.Fatalf("zero-filled numeric member must not reject: %v", err)
	}
	// sock non-empty counts; port zero does not -> pass.
	if _, err := c.Invoke(context.Background(), "net.bind", map[string]any{"sock": "/tmp/x"}, ActorModel); err != nil {
		t.Fatalf("sock selected with zero port must pass: %v", err)
	}
	// Both selected -> ambiguity rejected.
	if _, err := c.Invoke(context.Background(), "net.bind", map[string]any{"port": 8080, "sock": "/tmp/x"}, ActorModel); !errors.Is(err, ErrSelector) {
		t.Fatalf("port+sock both non-zero: expected ErrSelector, got %v", err)
	}
}

// TestSelectorGroupRejectsDurationZero verifies a zero time.Duration does not
// count as selected, guarding the named-int64 type switch (Duration never
// matches case int64).
func TestSelectorGroupRejectsDurationZero(t *testing.T) {
	h := &captureHandler{}
	c := NewCatalog()
	if err := c.Add(NewOperation(OperationSpec{
		Name: "net.hold", Title: "Hold", Summary: "hold",
		Category: "net", Safety: SafetyMutate,
		Interaction: InteractionAgentSafe, Visibility: VisibilityModel,
		Args: []OperationArg{
			{Name: "retry", Type: ArgTypeDuration, SelectionGroup: "mode", Default: "0s"},
			{Name: "name", Type: ArgTypeString, SelectionGroup: "mode"},
		},
		Handler: h,
	})); err != nil {
		t.Fatalf("Add: %v", err)
	}
	// retry default-filled to 0s must NOT count as selected; name absent -> pass.
	if _, err := c.Invoke(context.Background(), "net.hold", map[string]any{}, ActorModel); err != nil {
		t.Fatalf("zero duration must not reject: %v", err)
	}
	// name non-empty counts; retry 0s does not -> pass.
	if _, err := c.Invoke(context.Background(), "net.hold", map[string]any{"name": "x"}, ActorModel); err != nil {
		t.Fatalf("name selected with zero duration must pass: %v", err)
	}
	// Both selected -> ambiguity rejected.
	if _, err := c.Invoke(context.Background(), "net.hold", map[string]any{"retry": 5 * time.Second, "name": "x"}, ActorModel); !errors.Is(err, ErrSelector) {
		t.Fatalf("retry+name both non-zero: expected ErrSelector, got %v", err)
	}
}

// TestSelectorGroupErrSelectorIdempotent verifies errors.Is works even when the
// error is wrapped by normalize's context.
func TestSelectorGroupErrSelectorIdempotent(t *testing.T) {
	h := &captureHandler{}
	c := newSelectorOp(h)
	_, err := c.Invoke(context.Background(), "pin.remove", map[string]any{"cids": []string{"x"}, "all": true}, ActorModel)
	if !errors.Is(err, ErrSelector) {
		t.Fatalf("errors.Is(err, ErrSelector) = false, err = %v", err)
	}
}

// TestSelectorGroupViaNormalizeOperationInput confirms direct callers that reuse
// NormalizeOperationInput (e.g. MCP vault handlers routing around Invoke) get
// the same gate.
func TestSelectorGroupViaNormalizeOperationInput(t *testing.T) {
	h := &captureHandler{}
	op := NewOperation(OperationSpec{
		Name: "pin.remove", Title: "Remove", Summary: "remove",
		Category: "pin", Safety: SafetyMutate,
		Interaction: InteractionAgentSafe, Visibility: VisibilityModel,
		Args: []OperationArg{
			{Name: "cids", Type: ArgTypeStringSlice, SelectionGroup: "remove"},
			{Name: "all", Type: ArgTypeBool, SelectionGroup: "remove", Default: "false"},
		},
		Handler: h,
	})
	if _, err := NormalizeOperationInput(op, map[string]any{"cids": []string{"x"}, "all": true}); !errors.Is(err, ErrSelector) {
		t.Fatalf("ambiguous input through NormalizeOperationInput: expected ErrSelector, got %v", err)
	}
	got, err := NormalizeOperationInput(op, map[string]any{"cids": []string{"x"}})
	if err != nil {
		t.Fatalf("valid input rejected: %v", err)
	}
	if v, _ := got["all"].(bool); v {
		t.Fatalf("all should default to false, got %v", got["all"])
	}
}

// TestNormalizeOperationInputRejectsUnknownArg verifies that an unrecognized
// parameter (e.g. a typo like `limit` when the declared arg is `page-size`) is
// rejected loudly instead of silently ignored as a no-op. Declared args, their
// camelCase aliases, and the reserved plumbing keys must still be accepted.
func TestNormalizeOperationInputRejectsUnknownArg(t *testing.T) {
	op := NewOperation(OperationSpec{
		Name: "ops.list", Title: "List", Summary: "list",
		Category: "ops", Safety: SafetyRead,
		Interaction: InteractionAgentSafe, Visibility: VisibilityBoth,
		Args: []OperationArg{
			{Name: "page-size", Type: ArgTypeInt, Default: "0"},
			{Name: "watch", Type: ArgTypeBool, Default: "false"},
		},
		Handler: &captureHandler{},
	})

	// Declared arg (kebab) and camelCase alias pass.
	for _, in := range []map[string]any{
		{"page-size": 25},
		{"pageSize": 25},
		{"watch": true},
		{},
	} {
		if _, err := NormalizeOperationInput(op, in); err != nil {
			t.Fatalf("valid input %v rejected: %v", in, err)
		}
	}

	// Reserved plumbing keys pass the strict check, but only the auth-token
	// override is intended to reach a handler. request_state is a transport-only
	// key and must be stripped from the returned input so it never leaks into a
	// handler's map.
	out, err := NormalizeOperationInput(op, map[string]any{
		ReservedAuthTokenKey:    "tok",
		ReservedRequestStateKey: "state",
	})
	if err != nil {
		t.Fatalf("reserved keys rejected: %v", err)
	}
	if _, ok := out[ReservedAuthTokenKey]; !ok {
		t.Fatal("auth-token override must remain in normalized input for the handler's service selector")
	}
	if _, ok := out[ReservedRequestStateKey]; ok {
		t.Fatal("request_state must be stripped from normalized input, not reach the handler")
	}

	// An unknown/typo'd key must be rejected loudly.
	_, err = NormalizeOperationInput(op, map[string]any{"limit": 5})
	if err == nil {
		t.Fatal("unknown argument 'limit' must be rejected, not silently ignored")
	}
	if !strings.Contains(err.Error(), `"limit"`) {
		t.Fatalf("error should name the unknown argument, got %v", err)
	}
}

// TestNormalizeOperationInputReservedKeyArgCollision verifies that a key whose
// name collides with a reserved plumbing key (request_state) is NOT stripped
// when an operation declares it as a real OperationArg: the declared arg must
// win over the transport reservation so its value reaches the Handler.
func TestNormalizeOperationInputReservedKeyArgCollision(t *testing.T) {
	// An op that genuinely declares an arg named request_state (transport
	// reservation name, but declared here as a real data arg).
	op := NewOperation(OperationSpec{
		Name: "ops.collide", Title: "Collide", Summary: "declared request_state arg",
		Category: "ops", Safety: SafetyRead,
		Interaction: InteractionAgentSafe, Visibility: VisibilityBoth,
		Args: []OperationArg{
			{Name: "request_state", Type: ArgTypeString},
		},
		Handler: &captureHandler{},
	})

	out, err := NormalizeOperationInput(op, map[string]any{"request_state": "declared-value"})
	if err != nil {
		t.Fatalf("declared request_state arg rejected: %v", err)
	}
	// Guard must keep the declared arg's value rather than stripping it as
	// transport plumbing.
	got, ok := out["request_state"]
	if !ok {
		t.Fatal("declared request_state arg was stripped from normalized input")
	}
	if got != "declared-value" {
		t.Fatalf("declared request_state arg value corrupted: got %v, want %q", got, "declared-value")
	}
}
