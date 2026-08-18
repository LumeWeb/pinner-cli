package model

// ElicitationSpec describes an interactive input request a handler wants from
// the connected client. It is SDK-neutral; the SDK seam (officialToolResult)
// converts it into the wire's InputRequests. Exactly one of FormSchema or URL
// should be set.
type ElicitationSpec struct {
	// ID is the server-assigned request id echoed by the client in
	// InputResponses on retry. Use a stable key such as the element being
	// collected.
	ID string `json:"id"`

	// Message is the prompt presented to the user alongside the form/URL.
	Message string `json:"message,omitempty"`

	// FormSchema is a JSON Schema (object) describing the fields to render as
	// a form. It may be a *jsonschema.Schema, json.RawMessage, or a
	// map[string]any — anything that JSON-marshals to valid JSON Schema.
	FormSchema any `json:"requestedSchema,omitempty"`

	// URL, when set, switches to URL-mode elicitation: the client directs the
	// user to open URL out of band.
	URL string `json:"url,omitempty"`

	// ElicitationID identifies an out-of-band URL flow.
	ElicitationID string `json:"elicitationId,omitempty"`

	// RequestState is opaque state echoed back by the client on the retried
	// call. Handlers use it to carry context (e.g. a session id) across the
	// round-trip where arguments are otherwise lost.
	RequestState string `json:"requestState,omitempty"`
}

// FormElicitation is a convenience constructor for a form-mode elicitation.
func FormElicitation(id, message string, schema any) ElicitationSpec {
	return ElicitationSpec{ID: id, Message: message, FormSchema: schema}
}
