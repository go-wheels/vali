package vali

import (
	"regexp"
	"slices"
	"strings"
)

var localeSyntax = regexp.MustCompile("^[a-z][a-z0-9]*(?:-[a-z0-9]+)*$")

func normalizeLocale(locale string) string {
	locale = strings.ReplaceAll(strings.ToLower(strings.TrimSpace(locale)), "_", "-")
	if locale == "zh-hans" {
		return "zh-cn"
	}
	return locale
}

func parentLocale(locale string) string {
	if i := strings.LastIndexByte(locale, '-'); i >= 0 {
		return normalizeLocale(locale[:i])
	}
	return ""
}

func (v *Validator) localeFallbacks(locale string) []string {
	var locales []string
	for _, candidate := range []string{normalizeLocale(locale), v.fallbackLocale, "en"} {
		for candidate != "" && !slices.Contains(locales, candidate) {
			locales = append(locales, candidate)
			candidate = parentLocale(candidate)
		}
	}
	return locales
}
