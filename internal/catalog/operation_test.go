package catalog

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

// testHandler is a minimal Handler implementation used to verify the
// Operation.Handler() round-trip.
type testHandler struct{}

func (testHandler) Execute(_ context.Context, _ map[string]any) (any, error) {
	return nil, errors.New("unused")
}

func TestNewOperationRoundTrip(t *testing.T) {
	spec := OperationSpec{
		Name:             "websites.dns.add",
		Title:            "Add DNS record",
		Summary:          "Adds a DNS record to a website zone.",
		Description:      "Adds a new DNS record of the given type to the website's zone.",
		AgentDescription: "Add a DNS record (type, name, content) to a website zone.",
		Args: []OperationArg{
			{
				Name:      "domain",
				Type:      ArgTypeString,
				Required:  true,
				Help:      "website domain",
				AgentHelp: "domain to add the record to",
			},
			{
				Name:      "type",
				Type:      ArgTypeString,
				Required:  true,
				Enum:      []string{"A", "AAAA", "CNAME", "TXT"},
				Help:      "record type",
				AgentHelp: "DNS record type (A, AAAA, CNAME, TXT)",
			},
			{
				Name:      "ttl",
				Type:      ArgTypeInt,
				Default:   "3600",
				Help:      "TTL in seconds",
				AgentHelp: "record TTL in seconds",
			},
			{
				Name:      "priority",
				Type:      ArgTypeFloat,
				Help:      "record priority",
				AgentHelp: "record priority",
			},
			{
				Name:      "enabled",
				Type:      ArgTypeBool,
				Required:  true,
				Help:      "whether the record is enabled",
				AgentHelp: "enable the record after creation",
			},
			{
				Name:      "ttl_duration",
				Type:      ArgTypeDuration,
				Help:      "TTL as a duration",
				AgentHelp: "TTL as a Go duration string",
			},
			{
				Name:      "aliases",
				Type:      ArgTypeStringSlice,
				Help:      "additional hostnames",
				AgentHelp: "additional hostnames for the record",
			},
			{
				Name:      "secret",
				Type:      ArgTypeString,
				Required:  true,
				Sensitive: true,
				Help:      "token",
				AgentHelp: "token (masked)",
			},
		},
		Positional:  "DOMAIN TYPE",
		Safety:      SafetyMutate,
		Interaction: InteractionAgentSafe,
		Visibility:  VisibilityBoth,
		Category:    "websites",
		Handler:     testHandler{},
	}

	op := NewOperation(spec)

	// Scalar fields.
	got := map[string]any{
		"Name":             op.Name(),
		"Title":            op.Title(),
		"Summary":          op.Summary(),
		"Description":      op.Description(),
		"AgentDescription": op.AgentDescription(),
		"Positional":       op.Positional(),
		"Safety":           op.Safety(),
		"Interaction":      op.Interaction(),
		"Visibility":       op.Visibility(),
		"Category":         op.Category(),
	}
	want := map[string]any{
		"Name":             spec.Name,
		"Title":            spec.Title,
		"Summary":          spec.Summary,
		"Description":      spec.Description,
		"AgentDescription": spec.AgentDescription,
		"Positional":       spec.Positional,
		"Safety":           spec.Safety,
		"Interaction":      spec.Interaction,
		"Visibility":       spec.Visibility,
		"Category":         spec.Category,
	}
	for field, wantVal := range want {
		if gotVal := got[field]; !reflect.DeepEqual(gotVal, wantVal) {
			t.Errorf("Operation.%s() = %v, want %v", field, gotVal, wantVal)
		}
	}

	// Args round-trip.
	if !reflect.DeepEqual(op.Args(), spec.Args) {
		t.Errorf("Operation.Args() = %#v, want %#v", op.Args(), spec.Args)
	}

	// Handler round-trip.
	if op.Handler() != spec.Handler {
		t.Errorf("Operation.Handler() = %v, want %v", op.Handler(), spec.Handler)
	}
}

func TestOperationZeroValues(t *testing.T) {
	op := NewOperation(OperationSpec{})
	if op.Name() != "" || op.Safety() != SafetyRead || op.Visibility() != VisibilityModel || op.Interaction() != InteractionAgentSafe {
		t.Errorf("zero-value Operation should default Safety->Read, Visibility->Model, empty strings; got safety=%v visibility=%v interaction=%v",
			op.Safety(), op.Visibility(), op.Interaction())
	}
	if op.Handler() != nil {
		t.Errorf("zero-value Operation.Handler() should be nil, got %v", op.Handler())
	}
}
