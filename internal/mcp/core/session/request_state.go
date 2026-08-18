package session

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"time"
)

// This file implements a TypeScript-style HMAC-signed `requestState` codec for
// the 2026-07-28 multi-round-trip (MRTR) elicitation flow.
//
// Per the canonical TS SDK (createRequestStateCodec), `requestState` round-trips
// through the client and is attacker-controlled input on re-entry, so the spec
// has an integrity MUST on it. Wire shape mirrors the TS codec, minus the
// optional bind tag (we have no per-principal binding):
//
//	"v1." base64url({"p":<payload>,"exp":<unixSeconds>}) "." base64url(HMAC)
//
// `mintRequestState` seals a session id into an opaque wire string that the
// client echoes back; `verifyRequestState` fails closed on a bad MAC, malformed
// envelope, or expired value, returning the original session id. The same key
// must be available to every instance that may receive an echoed value.

const requestStatePrefix = "v1."

// RequestStatePrefix is the versioned wire prefix every signed requestState
// begins with ("v1.<body>.<mac>"). It is exported for consumers that need to
// inspect or manipulate the wire shape (e.g. tests).
const RequestStatePrefix = requestStatePrefix

// requestStateTTL bounds how long a minted requestState stays valid. It MUST
// exceed the wizard session TTL (DefaultSessionTTL = 30m) because on a form
// retry the requestState is the sole carrier of the session id; if it expired
// first, an otherwise-valid session would be stranded with "session_id is
// required".
const requestStateTTL = 35 * time.Minute

var errRequestState = errors.New("invalid requestState")

// mintRequestState seals payload into a signed wire string.
func mintRequestState(key []byte, payload string, now time.Time) (string, error) {
	env, err := json.Marshal(struct {
		P   string `json:"p"`
		Exp int64  `json:"exp"`
	}{
		P:   payload,
		Exp: now.Add(requestStateTTL).Unix(),
	})
	if err != nil {
		return "", err
	}
	body := base64.RawURLEncoding.EncodeToString(env)
	mac := signRequestState(key, body)
	return requestStatePrefix + body + "." + base64.RawURLEncoding.EncodeToString(mac), nil
}

// verifyRequestState validates state (MAC, shape, expiry) and returns the
// original payload. Any verification failure yields errRequestState; the
// decoded payload is intentionally NEVER returned in an error so it cannot
// leak to a tampering client (mirrors the TS codec's opaque reason codes).
func verifyRequestState(key []byte, state string, now time.Time) (string, error) {
	if !hasPrefix(state, requestStatePrefix) {
		return "", errRequestState
	}
	// shape: "v1." <body> "." <mac> ; the MAC must be checked FIRST.
	dot := lastIndexByte(state, '.')
	if dot < len(requestStatePrefix) {
		return "", errRequestState
	}
	body := state[len(requestStatePrefix):dot]
	macB64 := state[dot+1:]
	mac, err := base64.RawURLEncoding.DecodeString(macB64)
	if err != nil {
		return "", errRequestState
	}
	if !hmac.Equal(mac, signRequestState(key, body)) {
		return "", errRequestState
	}
	// The body decoded after a good MAC is by construction our own JSON.
	var env struct {
		P   string `json:"p"`
		Exp int64  `json:"exp"`
	}
	if err := json.Unmarshal(mustB64(body), &env); err != nil {
		return "", errRequestState
	}
	if env.P == "" || env.Exp < now.Unix() {
		return "", errRequestState
	}
	return env.P, nil
}

// signRequestState computes the HMAC over the version-prefixed body so the
// version tag is authenticated (a v1 token cannot be transplanted to a future
// v2 codec under the same key).
func signRequestState(key []byte, body string) []byte {
	m := hmac.New(sha256.New, key)
	m.Write([]byte(requestStatePrefix))
	m.Write([]byte(body))
	return m.Sum(nil)
}

func hasPrefix(s, p string) bool {
	return len(s) >= len(p) && s[:len(p)] == p
}

func lastIndexByte(s string, b byte) int {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == b {
			return i
		}
	}
	return -1
}

func mustB64(s string) []byte {
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return nil
	}
	return b
}

// requestStateKey is a per-process secret used to sign/verify requestState
// tokens. A stdio MCP server is a single short-lived process that serves every
// round of a flow, so an ephemeral random key is correct (mirrors the TS
// codec's per-process default). If the server is ever split across processes or
// exposed publicly, this must become a stable, shared secret.
var requestStateKey = func() []byte {
	key := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		panic("mcp: failed to generate requestState key: " + err.Error())
	}
	return key
}()

// MintWizardRequestState seals a wizard session id into a signed requestState
// the client echoes back on the elicitation retry.
func MintWizardRequestState(sessionID string, now time.Time) (string, error) {
	return mintRequestState(requestStateKey, sessionID, now)
}

// VerifyWizardRequestState validates the echoed requestState and returns the
// session id it carries. It fails closed on tampering/expiry.
func VerifyWizardRequestState(state string, now time.Time) (string, error) {
	return verifyRequestState(requestStateKey, state, now)
}
