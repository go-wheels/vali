package vali

import (
	"cmp"
	"context"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

type acceptLanguageKey struct{}

// WithAcceptLanguage returns a context carrying one or more HTTP Accept-Language
// values. Validation selects an available catalog unless an explicit Locale is
// supplied in the validation options.
func WithAcceptLanguage(ctx context.Context, values ...string) context.Context {
	return context.WithValue(ctx, acceptLanguageKey{}, strings.Join(values, ","))
}

type languagePreference struct {
	locale  string
	quality float64
}

var languageQualitySyntax = regexp.MustCompile(`^(0(\.[0-9]{0,3})?|1(\.0{0,3})?)$`)

func parseLanguagePreference(entry string) (languagePreference, bool) {
	name, weight, weighted := strings.Cut(entry, ";")
	locale := normalizeLocale(name)
	if locale != "*" && !localeSyntax.MatchString(locale) {
		return languagePreference{}, false
	}
	quality := 1.0
	if weighted {
		key, value, ok := strings.Cut(weight, "=")
		value = strings.TrimSpace(value)
		if !ok || !strings.EqualFold(strings.TrimSpace(key), "q") || !languageQualitySyntax.MatchString(value) {
			return languagePreference{}, false
		}
		quality, _ = strconv.ParseFloat(value, 64)
	}
	return languagePreference{locale: locale, quality: quality}, true
}

func sortedLocaleNames(catalogs map[string]compiledCatalog) []string {
	names := make([]string, 0, len(catalogs))
	for name := range catalogs {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

func (v *Validator) negotiateLocale(header string) string {
	if header == "" {
		return ""
	}
	var preferences []languagePreference
	qualities := make(map[string]float64)
	for _, entry := range strings.Split(header, ",") {
		pref, ok := parseLanguagePreference(entry)
		if previous, seen := qualities[pref.locale]; ok && (!seen || pref.quality > previous) {
			qualities[pref.locale] = pref.quality
			preferences = append(preferences, pref)
		}
	}
	slices.SortStableFunc(preferences, func(a, b languagePreference) int {
		return cmp.Compare(b.quality, a.quality)
	})
	for _, pref := range preferences {
		if pref.quality == 0 || pref.quality != qualities[pref.locale] {
			continue
		}
		if pref.locale == "*" {
			for _, name := range v.localeFallbacks("") {
				if _, exists := v.catalogs[name]; exists && wildcardMatches(name, qualities) {
					return name
				}
			}
			for _, name := range v.localeNames {
				if wildcardMatches(name, qualities) {
					return name
				}
			}
			continue
		}
		for name := pref.locale; name != ""; name = parentLocale(name) {
			if _, exists := v.catalogs[name]; exists && !excludedLocale(name, qualities) {
				return name
			}
		}
		for _, name := range v.localeNames {
			if !strings.HasPrefix(name, pref.locale+"-") {
				continue
			}
			if quality, _ := matchingQuality(name, qualities); quality >= pref.quality {
				return name
			}
		}
	}
	return ""
}

func matchingQuality(locale string, qualities map[string]float64) (float64, bool) {
	for name := locale; name != ""; name = parentLocale(name) {
		if quality, ok := qualities[name]; ok {
			return quality, true
		}
	}
	return 0, false
}

func excludedLocale(locale string, qualities map[string]float64) bool {
	quality, ok := matchingQuality(locale, qualities)
	return ok && quality == 0
}

func wildcardMatches(locale string, qualities map[string]float64) bool {
	_, explicit := matchingQuality(locale, qualities)
	return !explicit
}
