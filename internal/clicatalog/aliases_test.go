package clicatalog

import (
	"context"

	opmesh "go.lumeweb.com/opmesh"
)

// This file re-exports the shared pinner catalog symbols used by the CLI
// compiler's test files, so the tests migrated with the compiler package
// unqualified (package clicatalog) without rewriting every reference.

type (
	Catalog       = opmesh.Catalog
	Operation     = opmesh.Operation
	OperationArg  = opmesh.OperationArg
	OperationSpec = opmesh.OperationSpec
	List          = opmesh.List
)

const (
	ArgTypeString       = opmesh.ArgTypeString
	ArgTypeBool         = opmesh.ArgTypeBool
	ArgTypeNullableBool = opmesh.ArgTypeNullableBool
	ArgTypeNullableInt  = opmesh.ArgTypeNullableInt
	ArgTypeInt          = opmesh.ArgTypeInt
	ArgTypeFloat        = opmesh.ArgTypeFloat
	ArgTypeDuration     = opmesh.ArgTypeDuration
	ArgTypeStringSlice  = opmesh.ArgTypeStringSlice

	SafetyRead        = opmesh.SafetyRead
	SafetyMutate      = opmesh.SafetyMutate
	SafetyDestructive = opmesh.SafetyDestructive

	InteractionAgentSafe = opmesh.InteractionAgentSafe
	InteractionHumanOnly = opmesh.InteractionHumanOnly

	VisibilityModel = opmesh.VisibilityModel
	VisibilityBoth  = opmesh.VisibilityBoth
)

// captureHandler is a Handler recording the last input it received, so tests
// can assert on the exact input the CLI action dispatched.
type captureHandler struct{ got map[string]any }

func (h *captureHandler) Execute(ctx context.Context, input map[string]any) (any, error) {
	h.got = input
	return h, nil
}

// markerHandler returns a fixed marker value from Execute, letting tests assert
// that a compiled Action actually reached the Handler.
type markerHandler struct {
	marker string
}

func (h markerHandler) Execute(_ context.Context, _ map[string]any) (any, error) {
	return h.marker, nil
}

func NewCatalog() Catalog { return opmesh.NewCatalog() }

func NewOperation(spec OperationSpec) Operation { return opmesh.NewOperation(spec) }
