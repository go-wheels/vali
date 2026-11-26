package vali

import (
	"cmp"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"
)

// Catalog contains the labels and messages for one locale.
type Catalog struct {
	// Labels translates keys supplied by label tags or FieldOptions.Label.
	Labels map[string]string `json:"labels" toml:"labels"`
	// Messages supplies default templates by rule name or Violation.MessageKey.
	Messages map[string]string `json:"messages" toml:"messages"`
	// FieldMessages overrides templates by concrete or wildcard field path and rule name.
	FieldMessages map[string]map[string]string `json:"field_messages" toml:"field_messages"`
	// FormMessages overrides templates by form alias, field path and rule name.
	FormMessages map[string]map[string]map[string]string `json:"form_messages" toml:"form_messages"`
	// FormLabels overrides field labels by form alias and field path.
	FormLabels map[string]map[string]string `json:"form_labels,omitempty" toml:"form_labels"`
}

type compiledCatalog struct {
	labels        map[string]string
	messages      map[string]string
	fieldMessages pathOverrides[map[string]string]
	formMessages  map[string]pathOverrides[map[string]string]
	formLabels    map[string]pathOverrides[string]
}

type pathOverrides[T any] struct {
	exact     map[string]T
	wildcards map[string][]pathOverride[T]
}

type pathOverride[T any] struct {
	path          string
	segments      []string
	wildcardCount int
	value         T
}

func compileCatalogs(custom map[string]Catalog) (map[string]compiledCatalog, error) {
	catalogs := builtinCatalogs()
	seen := make(map[string]bool, len(custom))
	for language, entries := range custom {
		language = normalizeLocale(language)
		if !localeSyntax.MatchString(language) || seen[language] {
			return nil, fmt.Errorf("invalid or duplicate locale %q", language)
		}
		seen[language] = true
		base := catalogs[language]
		if base.Messages == nil {
			base.Messages = make(map[string]string)
		}
		maps.Copy(base.Messages, entries.Messages)
		base.Labels = maps.Clone(entries.Labels)
		base.FieldMessages = entries.FieldMessages
		base.FormMessages = entries.FormMessages
		base.FormLabels = entries.FormLabels
		catalogs[language] = base
	}
	result := make(map[string]compiledCatalog, len(catalogs))
	for language, catalog := range catalogs {
		result[language] = compiledCatalog{
			labels:        catalog.Labels,
			messages:      catalog.Messages,
			fieldMessages: compilePathOverrides(catalog.FieldMessages, maps.Clone),
			formMessages:  compileFormOverrides(catalog.FormMessages, maps.Clone),
			formLabels:    compileFormOverrides(catalog.FormLabels, nil),
		}
	}
	return result, nil
}

func compileFormOverrides[T any](forms map[string]map[string]T, clone func(T) T) map[string]pathOverrides[T] {
	if len(forms) == 0 {
		return nil
	}
	compiled := make(map[string]pathOverrides[T], len(forms))
	for alias, fields := range forms {
		compiled[alias] = compilePathOverrides(fields, clone)
	}
	return compiled
}

func compilePathOverrides[T any](fields map[string]T, clone func(T) T) pathOverrides[T] {
	if len(fields) == 0 {
		return pathOverrides[T]{}
	}
	compiled := pathOverrides[T]{exact: make(map[string]T, len(fields))}
	for path, value := range fields {
		if clone != nil {
			value = clone(value)
		}
		compiled.exact[path] = value
		segments := strings.Split(path, ".")
		wildcardCount := 0
		for _, segment := range segments {
			if segment == "*" {
				wildcardCount++
			}
		}
		if wildcardCount > 0 {
			if compiled.wildcards == nil {
				compiled.wildcards = make(map[string][]pathOverride[T])
			}
			shape := wildcardIndexPath(path)
			compiled.wildcards[shape] = append(compiled.wildcards[shape], pathOverride[T]{path: path, segments: segments, wildcardCount: wildcardCount, value: value})
		}
	}
	for _, overrides := range compiled.wildcards {
		slices.SortFunc(overrides, func(a, b pathOverride[T]) int {
			if order := cmp.Compare(a.wildcardCount, b.wildcardCount); order != 0 {
				return order
			}
			return strings.Compare(a.path, b.path)
		})
	}
	return compiled
}

func (c pathOverrides[T]) match(path, pattern string, lookup func(T) (string, bool)) (string, bool) {
	if exact, ok := c.exact[path]; ok {
		if value, ok := lookup(exact); ok {
			return value, true
		}
	}
	if len(c.wildcards) == 0 || !strings.ContainsRune(pattern, '*') {
		return "", false
	}
	overrides := c.wildcards[wildcardIndexPath(pattern)]
	if len(overrides) == 0 {
		return "", false
	}
	parts := strings.Split(path, ".")
	patternParts := strings.Split(pattern, ".")
	for _, override := range overrides {
		value, ok := lookup(override.value)
		if !ok {
			continue
		}
		matches := true
		for i, segment := range override.segments {
			if segment != parts[i] && (segment != "*" || patternParts[i] != "*") {
				matches = false
				break
			}
		}
		if matches {
			return value, true
		}
	}
	return "", false
}

func (e *validationRun) label(path, pattern, key, name string) string {
	if e.formAlias == "" && key == "" {
		return name
	}
	for _, locale := range e.locales {
		catalog := e.v.catalogs[locale]
		if e.formAlias != "" {
			formLabels := catalog.formLabels[e.formAlias]
			if pattern == "" && len(formLabels.wildcards) > 0 {
				pattern = e.schema.pathPattern(path)
			}
			if label, ok := formLabels.match(path, pattern, labelValue); ok {
				return label
			}
		}
		if key != "" {
			if label, ok := catalog.labels[key]; ok {
				return label
			}
		}
	}
	if key == "" {
		return name
	}
	return key
}

func labelValue(label string) (string, bool) { return label, true }

func (e *validationRun) template(in RuleInput, rule, key string) string {
	ruleMessage := func(messages map[string]string) (string, bool) {
		message, ok := messages[rule]
		return message, ok
	}
	for _, locale := range e.locales {
		catalog := e.v.catalogs[locale]
		if e.formAlias != "" {
			if message, ok := catalog.formMessages[e.formAlias].match(in.Path, in.pattern, ruleMessage); ok {
				return message
			}
		}
		if message, ok := catalog.fieldMessages.match(in.Path, in.pattern, ruleMessage); ok {
			return message
		}
		if message, ok := catalog.messages[key]; ok {
			return message
		}
		if message, ok := catalog.messages[rule]; ok {
			return message
		}
	}
	for _, locale := range e.locales {
		if message, ok := e.v.catalogs[locale].messages["invalid"]; ok {
			return message
		}
	}
	return ":attribute is invalid."
}

var placeholderSyntax = regexp.MustCompile(":[a-zA-Z_][a-zA-Z_0-9]*")

func (e *validationRun) render(in RuleInput, rule Rule, violation *Violation) FieldError {
	label := e.label(in.Path, in.pattern, in.Label, in.name)
	params := make(map[string]string, len(violation.Params)+2)
	maps.Copy(params, violation.Params)
	params["attribute"] = label
	if other := violation.other; other.Path != "" {
		params["other"] = e.label(other.Path, "", other.Label, leafName(other.Path))
	}
	key := violation.MessageKey
	if key == "" {
		key = rule.name
	}
	message := placeholderSyntax.ReplaceAllStringFunc(e.template(in, rule.name, key), func(token string) string {
		if value, ok := params[token[1:]]; ok {
			return value
		}
		return token
	})
	return FieldError{Path: in.Path, Rule: rule.name, Label: label, Message: message, Params: params}
}

func builtinCatalogs() map[string]Catalog {
	return map[string]Catalog{
		"en": {Messages: map[string]string{
			"invalid":          ":attribute is invalid.",
			"required":         ":attribute is required.",
			"string":           ":attribute must be a string.",
			"boolean":          ":attribute must be a boolean.",
			"array":            ":attribute must be an array.",
			"integer":          ":attribute must be an integer.",
			"numeric":          ":attribute must be a number.",
			"email":            ":attribute must be a valid email address.",
			"url":              ":attribute must be a valid HTTP or HTTPS URL.",
			"cn_mobile":        ":attribute must be a valid mainland China mobile number.",
			"cn_id_card":       ":attribute must be a valid mainland China resident identity number.",
			"min":              ":attribute must be at least :min.",
			"max":              ":attribute must be at most :max.",
			"between":          ":attribute must be between :min and :max.",
			"size":             ":attribute must have a size of :size.",
			"in":               ":attribute must be one of the allowed values.",
			"not_in":           ":attribute is not an allowed value.",
			"regex":            ":attribute has an invalid format.",
			"same":             ":attribute must match :other.",
			"different":        ":attribute must differ from :other.",
			"confirmed":        ":attribute must match its confirmation (:other).",
			"required_if":      ":attribute is required for the selected :other.",
			"required_unless":  ":attribute is required for the selected :other.",
			"required_with":    ":attribute is required when :other is supplied.",
			"required_without": ":attribute is required when :other is missing.",
			"unique":           ":attribute has already been taken.",
			"exists":           "The selected :attribute does not exist.",
		}},
		"zh": {Messages: map[string]string{
			"invalid":          ":attribute 无效。",
			"required":         "请填写 :attribute。",
			"string":           ":attribute 必须是字符串。",
			"boolean":          ":attribute 必须是布尔值。",
			"array":            ":attribute 必须是数组。",
			"integer":          ":attribute 必须是整数。",
			"numeric":          ":attribute 必须是数字。",
			"email":            ":attribute 必须是有效的电子邮箱地址。",
			"url":              ":attribute 必须是有效的 HTTP 或 HTTPS 网址。",
			"cn_mobile":        ":attribute 必须是有效的中国大陆手机号码。",
			"cn_id_card":       ":attribute 必须是有效的中国大陆居民身份证号码。",
			"min":              ":attribute 必须至少为 :min。",
			"max":              ":attribute 必须至多为 :max。",
			"between":          ":attribute 必须介于 :min 和 :max 之间。",
			"size":             ":attribute 的大小必须为 :size。",
			"in":               ":attribute 必须是允许的选项之一。",
			"not_in":           ":attribute 所选的值不可用。",
			"regex":            ":attribute 的格式不正确。",
			"same":             ":attribute 必须与 :other 一致。",
			"different":        ":attribute 必须与 :other 不同。",
			"confirmed":        ":attribute 必须与 :other 一致。",
			"required_if":      "当前的 :other 选项要求填写 :attribute。",
			"required_unless":  "当前的 :other 选项要求填写 :attribute。",
			"required_with":    "填写 :other 时也需要填写 :attribute。",
			"required_without": "未填写 :other 时需要填写 :attribute。",
			"unique":           ":attribute 已被使用。",
			"exists":           "所选的 :attribute 不存在。",
		}},
	}
}
