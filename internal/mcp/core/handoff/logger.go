package handoff

import "go.uber.org/zap"

// log is the package-level default logger for hand-off endpoint lifecycle
// events (mint, consume, expire). It mirrors the hub's production logger; a
// caller can override a specific endpoint's logger with handoffEndpoint.WithLogger.
var log = zap.Must(zap.NewProduction())
