package credctx

import (
	"context"
	"testing"
)

// TestRoundTrip verifies a JWT stored with With is returned by From.
func TestRoundTrip(t *testing.T) {
	ctx := With(context.Background(), "portal.jwt.token")
	if got := From(ctx); got != "portal.jwt.token" {
		t.Fatalf("From(With(ctx, jwt)) = %q, want %q", got, "portal.jwt.token")
	}
}

// TestFromBareReturnsEmpty verifies From on a context with no credential set
// returns "".
func TestFromBareReturnsEmpty(t *testing.T) {
	if got := From(context.Background()); got != "" {
		t.Fatalf("From(bare ctx) = %q, want \"\"", got)
	}
}
