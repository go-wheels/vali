package vali

import (
	"strconv"
	"strings"
)

// AntDesignField is the error payload for one field in Ant Design Form.setFields.
// Name contains strings for field names and numbers for collection indexes.
type AntDesignField struct {
	Name   []any    `json:"name"`
	Errors []string `json:"errors"`
}

// AntDesignFields groups localized error messages by field path for
// Form.setFields. Fields and messages retain their input order. An empty input
// returns an empty array when marshaled to JSON.
//
// Paths use the validator's dot notation. Canonical non-negative integer
// segments in JavaScript's safe integer range are interpreted as list indexes.
func (failures FieldErrors) AntDesignFields() []AntDesignField {
	fields := make([]AntDesignField, 0)
	positions := make(map[string]int)
	for _, failure := range failures {
		position, exists := positions[failure.Path]
		if !exists {
			position = len(fields)
			positions[failure.Path] = position
			fields = append(fields, AntDesignField{Name: antDesignNamePath(failure.Path)})
		}
		fields[position].Errors = append(fields[position].Errors, failure.Message)
	}
	return fields
}

func antDesignNamePath(path string) []any {
	segments := strings.Split(path, ".")
	name := make([]any, len(segments))
	for i, segment := range segments {
		name[i] = segment
		if isPathIndex(segment) {
			// JavaScript numbers preserve integer precision through 2^53 - 1.
			if index, err := strconv.ParseUint(segment, 10, 53); err == nil {
				name[i] = index
			}
		}
	}
	return name
}
