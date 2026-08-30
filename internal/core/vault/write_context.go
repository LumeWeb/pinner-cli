package vault

// writeContext is the typed projection of the well-known write-context metadata
// keys (src, host, agent, profile). Decoding a metadata map into this struct
// yields typed string fields, so callers read fields instead of type-asserting
// map values at every use site. The durable source is the object's sealed
// metadata; the recorded columns are a reconciled cache, exactly like tags.
type writeContext struct {
	Src     string
	Host    string
	Agent   string
	Profile string
}

// decodeWriteContext projects the well-known write-context metadata keys onto a
// typed writeContext. Keys that are absent or not strings decode to empty
// strings.
func decodeWriteContext(m map[string]any) writeContext {
	if m == nil {
		return writeContext{}
	}
	return writeContext{
		Src:     strVal(m[MetaKeySrc]),
		Host:    strVal(m[MetaKeyHost]),
		Agent:   strVal(m["agent"]),
		Profile: strVal(m[MetaKeyProfile]),
	}
}

// WriteContextColumns projects the well-known write-context metadata keys onto
// the normalized File columns used for search. Keys not present (or not
// strings) yield empty strings.
func WriteContextColumns(m map[string]any) (source, host, agent string) {
	wc := decodeWriteContext(m)
	return wc.Src, wc.Host, wc.Agent
}

// WriteContextProfile returns the profile recorded in a write-context metadata
// map, empty when absent or not a string. It is the profile the tool surface
// stamps so vault service construction can route to the requested profile.
func WriteContextProfile(m map[string]any) string {
	return decodeWriteContext(m).Profile
}

func strVal(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}
