package vali

// FieldError describes one failed rule. Path identifies the field and Rule is
// the name of the rule. Message and Label use the selected locale.
type FieldError struct {
	Path    string            `json:"path"`
	Rule    string            `json:"rule"`
	Label   string            `json:"label"`
	Message string            `json:"message"`
	Params  map[string]string `json:"params,omitempty"`
}

// FieldErrors contains rule failures in validation order. An empty result
// means validation passed when the call's error is nil.
type FieldErrors []FieldError

// Valid reports whether validation produced zero rule failures.
func (failures FieldErrors) Valid() bool { return len(failures) == 0 }

// ForPath returns the failures for an exact field path in validation order.
func (failures FieldErrors) ForPath(path string) FieldErrors {
	var matches FieldErrors
	for _, failure := range failures {
		if failure.Path == path {
			matches = append(matches, failure)
		}
	}
	return matches
}
