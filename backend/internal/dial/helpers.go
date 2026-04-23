package dial

import "strings"

// extractJSON extracts a string value from a flat JSON object by key.
// Used to avoid importing encoding/json in hot-path client code.
// For example: extractJSON(`{"sid":"CA123","status":"queued"}`, "sid") → "CA123"
func extractJSON(body, key string) string {
	needle := `"` + key + `"`
	idx := strings.Index(strings.ToLower(body), strings.ToLower(needle))
	if idx < 0 {
		return ""
	}
	rest := body[idx+len(needle):]
	rest = strings.TrimSpace(rest)
	if len(rest) == 0 || rest[0] != ':' {
		return ""
	}
	rest = strings.TrimSpace(rest[1:])
	if len(rest) == 0 {
		return ""
	}
	if rest[0] == '"' {
		end := strings.Index(rest[1:], `"`)
		if end < 0 {
			return ""
		}
		return rest[1 : end+1]
	}
	// numeric / bare value
	end := strings.IndexAny(rest, ",}")
	if end < 0 {
		return strings.TrimSpace(rest)
	}
	return strings.TrimSpace(rest[:end])
}

// extractNestedJSON extracts a value from a singly-nested JSON object.
// e.g. extractNestedJSON(`{"Call":{"Sid":"CA1",...}}`, "Call", "Sid") → "CA1"
func extractNestedJSON(body, outer, inner string) string {
	outerKey := `"` + outer + `"`
	idx := strings.Index(body, outerKey)
	if idx < 0 {
		return ""
	}
	start := strings.Index(body[idx:], "{")
	if start < 0 {
		return ""
	}
	sub := body[idx+start:]
	end := strings.Index(sub, "}")
	if end < 0 {
		return ""
	}
	return extractJSON(sub[:end+1], inner)
}
