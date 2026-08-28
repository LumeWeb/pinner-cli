package vault

// WriteContextColumns projects the well-known write-context metadata keys onto
// the normalized File columns used for search. The durable source is the
// object's sealed metadata; these columns are a reconciled cache, exactly like
// tags. Keys not present (or not strings) yield empty strings.
func WriteContextColumns(m map[string]any) (source, host, agent string) {
	if m == nil {
		return "", "", ""
	}
	return strVal(m[MetaKeySrc]), strVal(m[MetaKeyHost]), strVal(m["agent"])
}

func strVal(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}
