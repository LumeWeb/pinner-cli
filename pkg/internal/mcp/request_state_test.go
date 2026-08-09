package mcp

import (
	"testing"
	"time"
)

// TestRequestStateCodecRoundTrip verifies mint -> verify round-trips the
// payload and that tampering/expiry/malformed values fail closed, mirroring the
// TS SDK createRequestStateCodec integrity MUST.
func TestRequestStateCodec(t *testing.T) {
	key := []byte("0123456789abcdef0123456789abcdef") // >=32 bytes
	payload := "sess-42"
	now := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)

	state, err := mintRequestState(key, payload, now)
	if err != nil {
		t.Fatalf("mint: %v", err)
	}

	got, err := verifyRequestState(key, state, now)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if got != payload {
		t.Fatalf("payload mismatch: got %q want %q", got, payload)
	}

	// Wrong key must fail.
	if _, err := verifyRequestState([]byte("0"), state, now); err == nil {
		t.Fatal("expected failure with wrong key")
	}

	// Tampered last char must fail (bad MAC).
	tampered := state[:len(state)-1] + "A"
	if _, err := verifyRequestState(key, tampered, now); err == nil {
		t.Fatal("expected failure on tampered token")
	}

	// Expired must fail (past requestStateTTL; also > DefaultSessionTTL so a
	// session is never stranded).
	if _, err := verifyRequestState(key, state, now.Add(requestStateTTL+time.Minute)); err == nil {
		t.Fatal("expected failure on expired token")
	}

	// Not-expired boundary must pass.
	if _, err := verifyRequestState(key, state, now.Add(9*time.Minute)); err != nil {
		t.Fatalf("expected valid within TTL: %v", err)
	}

	// The requestState TTL must exceed the wizard session TTL so the token (the
	// sole carrier of the session id on a form retry) never outlives the token.
	if requestStateTTL <= DefaultSessionTTL {
		t.Fatalf("requestStateTTL (%v) must exceed DefaultSessionTTL (%v)", requestStateTTL, DefaultSessionTTL)
	}

	// Malformed must fail.
	if _, err := verifyRequestState(key, "garbage", now); err == nil {
		t.Fatal("expected failure on malformed token")
	}

	// The token must be opaque: it must not be the raw payload and must carry
	// the versioned wire shape (signed, not a bare value).
	if state == payload {
		t.Fatal("requestState must not be the raw session id")
	}
	if len(state) < len(requestStatePrefix)+2 || state[:len(requestStatePrefix)] != requestStatePrefix {
		t.Fatalf("requestState missing versioned shape: %q", state)
	}
}
