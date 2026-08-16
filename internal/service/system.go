package service

import "fmt"

// System represents an init system / service manager availability probe.
// Backends are registered in the order they should be preferred and the first
// whose Detect() returns true is chosen by New. This is the same
// detection-by-probe mechanism github.com/kardianos/service uses; pinner
// reimplements a minimal version so backends (systemd, launchd, Windows SCM,
// ...) can be added per platform without the package knowing about them.
type System interface {
	// String returns a description of the init system (e.g. "systemd").
	String() string
	// Detect returns true if this init system is available on the host.
	Detect() bool
	// New creates a Service for this init system from the given config.
	New(cfg Config) Service
}

// registry is the ordered list of candidate Systems, populated by each
// backend's init function so the package itself stays platform-agnostic.
var registry []System

// register appends a System to the detection candidates.
func register(s System) {
	registry = append(registry, s)
}

// New builds and returns the Service for the first System whose Detect() probe
// passes. It returns nil (with a descriptive error) when no init system is
// detected.
func New(cfg Config) (Service, error) {
	for _, sys := range registry {
		if sys.Detect() {
			return sys.New(cfg), nil
		}
	}
	return nil, fmt.Errorf("no supported service system detected on this host")
}

// AvailableSystems returns the registered System candidates, in detection order.
func AvailableSystems() []System {
	out := make([]System, len(registry))
	copy(out, registry)
	return out
}
