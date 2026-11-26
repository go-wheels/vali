package vali

import "strings"

func joinPath(parent, name string) string {
	if parent == "" {
		return name
	}
	return parent + "." + name
}

func leafName(path string) string {
	return path[strings.LastIndexByte(path, '.')+1:]
}

func validPathSegment(segment string) bool {
	return strings.TrimSpace(segment) != "" && !strings.ContainsAny(segment, "./*")
}

func validFieldPath(path string) bool {
	for _, segment := range strings.Split(path, ".") {
		if !validPathSegment(segment) {
			return false
		}
	}
	return true
}

func wildcardIndexPath(path string) string {
	segments := strings.Split(path, ".")
	for i, segment := range segments {
		if isPathIndex(segment) {
			segments[i] = "*"
		}
	}
	return strings.Join(segments, ".")
}

func isPathIndex(segment string) bool {
	if segment == "" || (segment[0] == '0' && len(segment) > 1) {
		return false
	}
	for i := 0; i < len(segment); i++ {
		if segment[i] < '0' || segment[i] > '9' {
			return false
		}
	}
	return true
}
