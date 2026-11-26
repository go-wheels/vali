package vali

import (
	"context"
	"fmt"
	"net/mail"
	"net/url"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// RuleInput is the value passed to a custom rule. LookupField resolves related struct
// fields or delegates to FieldOptions.LookupField for field validation.
// Value contains the prepared input. Path and Label control error reporting.
type RuleInput struct {
	Path        string
	Label       string
	Value       any
	LookupField func(path string) (RelatedField, bool)

	name, pattern string
	normalized    any
}

// RelatedField is returned by RuleInput.LookupField. A known field with a missing
// value is represented by a RelatedField with a nil Value and a true result.
type RelatedField struct {
	Path  string
	Label string
	Value any
}

// Violation describes a failed rule. An empty MessageKey selects the rule name.
type Violation struct {
	MessageKey string
	Params     map[string]string

	other RelatedField
}

// RuleCheckFunc returns nil on success, a Violation on invalid input, or an error
// when the check cannot complete. A check shared between calls must be safe for concurrent use.
type RuleCheckFunc func(context.Context, RuleInput) (*Violation, error)

// RuleSpec defines a custom rule.
// Implicit checks also run for blank strings and for nil values marked nullable.
type RuleSpec struct {
	Check    RuleCheckFunc
	Implicit bool
}

// RuleFactory receives the decoded rule arguments. It must be safe to call concurrently.
type RuleFactory func(args []string) (RuleSpec, error)

// Rule is a reusable compiled check or field modifier.
type Rule struct {
	name     string
	implicit bool
	refs     []string
	check    func(*validationRun, RuleInput, bool) (*Violation, error)
}

// Name returns the name of the rule.
func (r Rule) Name() string { return r.name }

// NewRule wraps a custom check for direct use in StructOptions.ExtraRules or
// Validator.ValidateFieldWithRules. The name identifies field errors and templates.
func NewRule(name string, spec RuleSpec) (Rule, error) {
	if err := validateCustomRuleName(name); err != nil {
		return Rule{}, err
	}
	if spec.Check == nil {
		return Rule{}, fmt.Errorf("%s: missing rule check", name)
	}
	return Rule{name: name, implicit: spec.Implicit, check: func(e *validationRun, in RuleInput, _ bool) (*Violation, error) {
		return spec.Check(e.ctx, in)
	}}, nil
}

func validateCustomRuleName(name string) error {
	if !ruleNameSyntax.MatchString(name) {
		return fmt.Errorf("invalid custom rule %q", name)
	}
	if _, reserved := builtinArgumentBounds[name]; reserved {
		return fmt.Errorf("custom rule %q conflicts with a built-in rule", name)
	}
	return nil
}

// CompileRule builds a rule from individual decoded arguments.
func (v *Validator) CompileRule(name string, args ...string) (Rule, error) {
	if factory, ok := v.ruleFactories[name]; ok {
		spec, err := factory(slices.Clone(args))
		if err != nil {
			return Rule{}, fmt.Errorf("%s: %w", name, err)
		}
		return NewRule(name, spec)
	}

	rule, err := compileBuiltinRule(name, args)
	if err != nil {
		return Rule{}, fmt.Errorf("%s: %w", name, err)
	}
	return rule, nil
}

// CompileRules compiles a pipe-delimited expression using validate tag syntax.
// Rule combinations and types are checked when the rules are applied.
func (v *Validator) CompileRules(expression string) ([]Rule, error) {
	parts, err := parseExpression(expression)
	if err != nil {
		return nil, err
	}
	rules := make([]Rule, 0, len(parts))
	for _, part := range parts {
		rule, err := v.CompileRule(part.name, part.args...)
		if err != nil {
			return nil, err
		}
		rules = append(rules, rule)
	}
	return rules, nil
}

var builtinArgumentBounds = map[string][2]int{
	"bail": {0, 0}, "nullable": {0, 0}, "dive": {0, 0},
	"required": {0, 0}, "string": {0, 0}, "boolean": {0, 0}, "array": {0, 0},
	"numeric": {0, 0}, "integer": {0, 0}, "email": {0, 0}, "url": {0, 0},
	"cn_mobile": {0, 0}, "cn_id_card": {0, 0},
	"min": {1, 1}, "max": {1, 1}, "between": {2, 2}, "size": {1, 1},
	"in": {1, -1}, "not_in": {1, -1}, "regex": {1, 1},
	"same": {1, 1}, "different": {1, 1}, "confirmed": {1, 1},
	"required_if": {2, -1}, "required_unless": {2, -1},
	"required_with": {1, -1}, "required_without": {1, -1},
	"unique": {2, 2}, "exists": {2, 2},
}

func compileBuiltinRule(name string, args []string) (Rule, error) {
	count, ok := builtinArgumentBounds[name]
	if !ok {
		return Rule{}, fmt.Errorf("unknown rule")
	}
	if len(args) < count[0] || (count[1] >= 0 && len(args) > count[1]) {
		switch {
		case count[0] == count[1]:
			return Rule{}, fmt.Errorf("expected %d arguments, got %d", count[0], len(args))
		default:
			return Rule{}, fmt.Errorf("expected at least %d arguments, got %d", count[0], len(args))
		}
	}

	rule := Rule{name: name}
	switch name {
	case "bail", "nullable", "dive":
		return rule, nil
	case "min", "max", "between", "size":
		return compileRangeRule(rule, args)
	case "same", "different", "confirmed", "required_if", "required_unless", "required_with", "required_without":
		return compileRelatedRule(rule, args)
	case "unique", "exists":
		return compileDatabaseRule(rule, args)
	case "regex":
		pattern, err := regexp.Compile(args[0])
		if err != nil {
			return Rule{}, err
		}
		rule.check = checkValue(func(value any) bool {
			raw, ok := stringValue(value)
			return ok && pattern.MatchString(raw)
		})
	case "in", "not_in":
		allowed := slices.Clone(args)
		rule.check = checkValue(func(value any) bool {
			raw, ok := scalarText(value)
			return ok && (slices.Contains(allowed, raw) == (name == "in"))
		})
	default:
		rule.implicit = name == "required"
		rule.check = checkValue(simpleChecks[name])
	}
	return rule, nil
}

func checkValue(check func(any) bool) func(*validationRun, RuleInput, bool) (*Violation, error) {
	return func(_ *validationRun, in RuleInput, _ bool) (*Violation, error) {
		if check(in.normalized) {
			return nil, nil
		}
		return &Violation{}, nil
	}
}

var simpleChecks = map[string]func(any) bool{
	"required": func(value any) bool { return !emptyValue(value) },
	"string":   func(value any) bool { _, ok := stringValue(value); return ok },
	"boolean":  isBoolean,
	"array": func(value any) bool {
		kind := reflect.ValueOf(value).Kind()
		return kind == reflect.Array || kind == reflect.Slice
	},
	"numeric":    func(value any) bool { _, ok := numberValue(value); return ok },
	"integer":    isInteger,
	"email":      isEmail,
	"url":        isURL,
	"cn_mobile":  isCNMobile,
	"cn_id_card": isCNIDCard,
}

func emptyValue(value any) bool {
	if value == nil || blankString(value) {
		return true
	}
	rv := reflect.ValueOf(value)
	switch rv.Kind() {
	case reflect.Array, reflect.Slice, reflect.Map:
		return rv.Len() == 0
	default:
		return false
	}
}

func blankString(value any) bool {
	raw, ok := stringValue(value)
	return ok && strings.TrimSpace(raw) == ""
}

func stringValue(value any) (string, bool) {
	rv := reflect.ValueOf(value)
	if rv.Kind() != reflect.String {
		return "", false
	}
	return rv.String(), true
}

func scalarText(value any) (string, bool) {
	rv := reflect.ValueOf(value)
	switch rv.Kind() {
	case reflect.String:
		return rv.String(), true
	case reflect.Bool:
		return strconv.FormatBool(rv.Bool()), true
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return strconv.FormatInt(rv.Int(), 10), true
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return strconv.FormatUint(rv.Uint(), 10), true
	case reflect.Float32, reflect.Float64:
		return strconv.FormatFloat(rv.Float(), 'g', -1, rv.Type().Bits()), true
	default:
		return "", false
	}
}

func isBoolean(value any) bool {
	rv := reflect.ValueOf(value)
	switch rv.Kind() {
	case reflect.Bool:
		return true
	case reflect.String:
		return rv.String() == "0" || rv.String() == "1"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return rv.Int() == 0 || rv.Int() == 1
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return rv.Uint() <= 1
	default:
		return false
	}
}

func isEmail(value any) bool {
	raw, ok := stringValue(value)
	if !ok {
		return false
	}
	address, err := mail.ParseAddress(raw)
	return err == nil && address.Name == "" && address.Address == raw
}

func isURL(value any) bool {
	raw, ok := stringValue(value)
	if !ok || strings.IndexFunc(raw, unicode.IsSpace) >= 0 {
		return false
	}
	address, err := url.Parse(raw)
	return err == nil && address.Hostname() != "" &&
		(strings.EqualFold(address.Scheme, "https") || strings.EqualFold(address.Scheme, "http"))
}

func compileRangeRule(rule Rule, args []string) (Rule, error) {
	args = slices.Clone(args)
	first, ok := parseNumber(args[0])
	if !ok {
		return Rule{}, fmt.Errorf("expected a finite decimal bound")
	}
	var second decimal
	if rule.name == "between" {
		second, ok = parseNumber(args[1])
		if !ok || first.compare(second) > 0 {
			return Rule{}, fmt.Errorf("expected ordered, finite decimal bounds")
		}
	}
	rule.check = func(_ *validationRun, in RuleInput, numeric bool) (*Violation, error) {
		value, ok := measuredValue(in.normalized, numeric)
		if ok {
			cmp := value.compare(first)
			switch rule.name {
			case "min":
				ok = cmp >= 0
			case "max":
				ok = cmp <= 0
			case "size":
				ok = cmp == 0
			case "between":
				ok = cmp >= 0 && value.compare(second) <= 0
			}
		}
		if ok {
			return nil, nil
		}
		params := map[string]string{rule.name: args[0]}
		if rule.name == "between" {
			params = map[string]string{"min": args[0], "max": args[1]}
		}
		return &Violation{Params: params}, nil
	}
	return rule, nil
}

func measuredValue(value any, numeric bool) (decimal, bool) {
	rv := reflect.ValueOf(value)
	switch rv.Kind() {
	case reflect.String:
		if !numeric {
			return parseNumber(strconv.Itoa(utf8.RuneCountInString(rv.String())))
		}
	case reflect.Array, reflect.Slice, reflect.Map:
		return parseNumber(strconv.Itoa(rv.Len()))
	}
	return numberValue(value)
}

func compileRelatedRule(rule Rule, args []string) (Rule, error) {
	rule.implicit = strings.HasPrefix(rule.name, "required_")
	paths := args
	if rule.name == "required_if" || rule.name == "required_unless" {
		paths = paths[:1]
	}
	for _, path := range paths {
		if !validFieldPath(strings.TrimPrefix(path, "/")) {
			return Rule{}, fmt.Errorf("invalid related field path %q", path)
		}
	}
	rule.refs = slices.Clone(paths)
	choices := slices.Clone(args[len(paths):])
	rule.check = func(e *validationRun, in RuleInput, _ bool) (*Violation, error) {
		others := make([]RelatedField, 0, len(rule.refs))
		for _, path := range rule.refs {
			field, err := e.related(in, path)
			if err != nil {
				return nil, err
			}
			if field.Path == "" {
				field.Path = strings.TrimPrefix(path, "/")
			}
			field.Value = unpack(field.Value)
			others = append(others, field)
		}
		return checkRelatedFields(rule.name, in.normalized, others, choices), nil
	}
	return rule, nil
}

func checkRelatedFields(name string, current any, others []RelatedField, choices []string) *Violation {
	switch name {
	case "same", "confirmed", "different":
		if others[0].Value == nil || reflect.DeepEqual(current, others[0].Value) != (name != "different") {
			return &Violation{other: others[0]}
		}
	case "required_if", "required_unless":
		text, ok := scalarText(others[0].Value)
		matches := ok && slices.Contains(choices, text)
		if matches == (name == "required_if") && emptyValue(current) {
			return &Violation{other: others[0]}
		}
	case "required_with", "required_without":
		if emptyValue(current) {
			for _, field := range others {
				if emptyValue(field.Value) == (name == "required_without") {
					return &Violation{other: field}
				}
			}
		}
	}
	return nil
}
