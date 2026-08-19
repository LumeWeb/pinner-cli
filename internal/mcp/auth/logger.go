package auth

import "go.uber.org/zap"

// log is the package-level default logger for out-of-band flow events. It
// mirrors the hub's production logger; flows that need a specific logger set it
// explicitly.
var log = zap.Must(zap.NewProduction())
