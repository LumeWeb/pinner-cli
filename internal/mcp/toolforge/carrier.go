// carrier.go welds the extracted go.lumeweb.com/mcpforge DSL onto Pinner's
// CLI context type. mcpforge is generic over a FeatureCarrier context; the
// chain-era CLI vocabulary (hostenv.Feature/FeatureSet aliased onto
// mcpplane/model, hostenv.Predicate over PlatformProfile) is distinct from
// mcpforge's own types, so this file carries the infallible conversions:
// the feature strings are byte-identical between model.Feature and
// mcpforge.Feature, and a hostenv.Predicate is re-wrapped over the carrier —
// no information is lost in either direction.
//
// These conversions live at the shim boundary only: package-local, never
// crossing into hostenv or the model layer.
package toolforge

import (
	mcpforge "go.lumeweb.com/mcpforge"

	"go.lumeweb.com/pinner-cli/internal/mcp/hostenv"
)

// forgeCarrier adapts hostenv.PlatformProfile to the mcpforge.FeatureCarrier
// constraint so the extracted builders (desc/schema/guide/targets) can be
// specialized over the CLI platform profile without hostenv having to know
// about mcpforge. It carries the profile by value; every builder Resolve/
// Build call constructs one on the fly.
type forgeCarrier struct {
	profile hostenv.PlatformProfile
}

// FeatureSet exposes the profile's feature set in mcpforge's set mechanics.
// It is re-derived on every call (builders call it per resolution pass).
func (c forgeCarrier) FeatureSet() mcpforge.FeatureSet {
	return toForgeFeatureSet(c.profile.Features)
}

// forgeFeature re-wraps a CLI feature string as the mcpforge feature type.
// The values are byte-identical (the feature vocabulary is owned by the
// consumer; mcpforge only supplies the set mechanics).
func forgeFeature(f hostenv.Feature) mcpforge.Feature {
	return mcpforge.Feature(f)
}

// forgeFeatures re-wraps a CLI feature slice for mcpforge's multi-feature
// gates (WhenAll/WhenAny/WhenAnySep/ListWhenAll/...).
func forgeFeatures(feats []hostenv.Feature) []mcpforge.Feature {
	out := make([]mcpforge.Feature, len(feats))
	for i, f := range feats {
		out[i] = forgeFeature(f)
	}
	return out
}

// toForgeFeatureSet re-wraps a CLI feature set (model.Feature keys) as the
// mcpforge feature set type. The values are byte-identical, so this is a
// pure key re-typing; a nil set stays nil.
func toForgeFeatureSet(fs hostenv.FeatureSet) mcpforge.FeatureSet {
	if fs == nil {
		return nil
	}
	out := make(mcpforge.FeatureSet, len(fs))
	for f, on := range fs {
		out[forgeFeature(f)] = on
	}
	return out
}

// fromForgeFeatureSet is the inverse of toForgeFeatureSet, for Transform
// callbacks whose CLI signatures take a hostenv.FeatureSet.
func fromForgeFeatureSet(fs mcpforge.FeatureSet) hostenv.FeatureSet {
	if fs == nil {
		return nil
	}
	out := make(hostenv.FeatureSet, len(fs))
	for f, on := range fs {
		out[hostenv.Feature(f)] = on
	}
	return out
}

// forgePred re-wraps a CLI platform predicate over the mcpforge carrier so
// hostenv's predicate constructors (HostIs, TransportIs, SurfaceIs, HostedIs,
// And, Not) can gate mcpforge fragments directly. Callers only wrap concrete
// predicates; the nil check keeps the adapter total.
func forgePred(p hostenv.Predicate) mcpforge.Predicate[forgeCarrier] {
	if p == nil {
		return nil
	}
	return func(c forgeCarrier) bool { return p(c.profile) }
}
