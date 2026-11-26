package vali

import (
	"fmt"
	"strconv"
	"strings"
)

type expressionPart struct {
	name string
	args []string
}

// parseStructTags checks the raw tag before using any of its values.
func parseStructTags(raw string) (map[string]string, error) {
	var tags map[string]string
	for raw = strings.TrimLeft(raw, " "); raw != ""; raw = strings.TrimLeft(raw, " ") {
		end := 0
		for end < len(raw) && raw[end] > ' ' && raw[end] != ':' && raw[end] != '"' && raw[end] != 0x7f {
			end++
		}
		if end == 0 || end+1 >= len(raw) || raw[end] != ':' || raw[end+1] != '"' {
			return nil, fmt.Errorf("invalid Go field tag")
		}
		key := raw[:end]
		raw = raw[end+1:]
		end = 1
		for end < len(raw) && raw[end] != '"' {
			if raw[end] == '\\' {
				end++
			}
			end++
		}
		if end >= len(raw) {
			return nil, fmt.Errorf("unterminated Go field tag %q", key)
		}
		value, err := strconv.Unquote(raw[:end+1])
		if err != nil {
			return nil, fmt.Errorf("Go field tag %q: %w", key, err)
		}
		if _, exists := tags[key]; exists {
			return nil, fmt.Errorf("duplicate Go field tag %q", key)
		}
		if tags == nil {
			tags = make(map[string]string)
		}
		tags[key] = value
		raw = raw[end+1:]
		if raw != "" && raw[0] != ' ' {
			return nil, fmt.Errorf("missing space after Go field tag %q", key)
		}
	}
	return tags, nil
}

func parseExpression(raw string) ([]expressionPart, error) {
	var parts []expressionPart
	for {
		end := strings.IndexAny(raw, ":|")
		if end < 0 {
			end = len(raw)
		}
		part := expressionPart{name: strings.TrimSpace(raw[:end])}
		if part.name == "" {
			return nil, fmt.Errorf("empty rule name")
		}
		raw = raw[end:]
		if strings.HasPrefix(raw, ":") {
			for {
				arg, remaining, err := parseExpressionArgument(raw[1:])
				if err != nil {
					return nil, fmt.Errorf("%s: %w", part.name, err)
				}
				part.args = append(part.args, arg)
				raw = remaining
				if !strings.HasPrefix(raw, ",") {
					break
				}
			}
		}
		parts = append(parts, part)
		if raw == "" {
			return parts, nil
		}
		raw = raw[1:]
	}
}

func parseExpressionArgument(raw string) (string, string, error) {
	raw = strings.TrimSpace(raw)
	if !strings.HasPrefix(raw, "'") {
		end := strings.IndexAny(raw, ",|")
		if end < 0 {
			end = len(raw)
		}
		arg := strings.TrimSpace(raw[:end])
		if arg == "" || strings.ContainsRune(arg, '\'') {
			return "", "", fmt.Errorf("expected a non-empty bare argument or a single-quoted argument")
		}
		return arg, raw[end:], nil
	}

	var arg strings.Builder
	for i := 1; i < len(raw); i++ {
		if raw[i] != '\'' {
			arg.WriteByte(raw[i])
			continue
		}
		if i+1 < len(raw) && raw[i+1] == '\'' {
			arg.WriteByte('\'')
			i++
			continue
		}
		remaining := strings.TrimSpace(raw[i+1:])
		if remaining != "" && remaining[0] != ',' && remaining[0] != '|' {
			return "", "", fmt.Errorf("unexpected text after quoted argument")
		}
		return arg.String(), remaining, nil
	}
	return "", "", fmt.Errorf("unterminated quoted argument")
}
