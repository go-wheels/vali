package vali

import (
	"context"
	"fmt"
	"reflect"
)

// StructOptions configures one struct validation. The zero value uses defaults.
type StructOptions struct {
	// Locale overrides the language preferences supplied by WithAcceptLanguage.
	Locale string
	// PrepareForm can replace the form before its prepare tags are applied.
	PrepareForm Preprocessor
	// ExtraRules appends rules by root-relative field path. Use * for list indexes.
	ExtraRules map[string][]Rule
	// UniqueExclusions identifies rows to ignore by concrete field path.
	UniqueExclusions map[string]RowExclusion
}

// FieldOptions configures one field validation. The zero value uses defaults.
type FieldOptions struct {
	// Locale overrides the language preferences supplied by WithAcceptLanguage.
	Locale string
	// Path is the concrete dot-separated path used in errors. It defaults to "value".
	Path string
	// Label is a catalog label key, with the given text as its fallback.
	Label string
	// LookupField resolves references in rules such as same and required_if.
	// A known but missing field is returned with a nil Value and true.
	LookupField func(path string) (RelatedField, bool)
	// PrepareValue can replace the value before its rules run.
	PrepareValue Preprocessor
	// UniqueExclusions identifies rows to ignore by concrete field path.
	UniqueExclusions map[string]RowExclusion
}

var defaultValidator, defaultValidatorErr = New(Config{})

// ValidateStruct evaluates a struct's tags with the built-in configuration.
// Repeated calls reuse compiled struct schemas.
func ValidateStruct(ctx context.Context, form any, options ...StructOptions) (FieldErrors, error) {
	if defaultValidatorErr != nil {
		return nil, defaultValidatorErr
	}
	return defaultValidator.ValidateStruct(ctx, form, options...)
}

// ValidateField evaluates a rule expression with the built-in configuration.
func ValidateField(ctx context.Context, value any, expression string, options ...FieldOptions) (FieldErrors, error) {
	if defaultValidatorErr != nil {
		return nil, defaultValidatorErr
	}
	return defaultValidator.ValidateField(ctx, value, expression, options...)
}

// ValidateStruct prepares a struct or non-nil struct pointer and evaluates its tags.
// Pass one StructOptions to customize the call, or omit it for defaults.
func (v *Validator) ValidateStruct(ctx context.Context, form any, options ...StructOptions) (FieldErrors, error) {
	opts, err := resolveOptions(options)
	if err != nil {
		return nil, err
	}
	e, err := v.newValidationRun(ctx, opts.Locale, opts.PrepareForm)
	if err != nil {
		return nil, err
	}
	e.uniqueExclusions = opts.UniqueExclusions
	form, err = e.prepareInput(form)
	if err != nil {
		return nil, err
	}
	root := reflect.ValueOf(form)
	record := indirectValue(root)
	if record.Kind() != reflect.Struct {
		return nil, fmt.Errorf("expected a struct or a non-nil struct pointer")
	}
	model, err := v.schemaFor(record.Type())
	if err != nil {
		return nil, err
	}
	targets, err := model.compileExtraRules(opts.ExtraRules)
	if err != nil {
		return nil, err
	}
	if model.hasPreparation {
		preparation := formPreparation{ctx: ctx, schema: model, copies: make(map[preparedValueReference]reflect.Value)}
		root, err = preparation.apply(root, "", nil, true)
		if err != nil {
			return nil, err
		}
		record = indirectValue(root)
	}
	e.schema, e.root = model, record
	e.formAlias = model.formAlias
	if err := e.walk(root, RuleInput{}, nil, true, targets); err != nil {
		return nil, err
	}
	return e.result()
}

// ValidateField evaluates a pipe-delimited rule expression such as
// "bail|required|email". Pass one FieldOptions or omit it for defaults.
// Use ValidateFieldWithRules to reuse rules across calls.
func (v *Validator) ValidateField(ctx context.Context, value any, expression string, options ...FieldOptions) (FieldErrors, error) {
	e, in, err := v.newFieldValidation(ctx, value, options)
	if err != nil {
		return nil, err
	}
	rules, err := v.CompileRules(expression)
	if err != nil {
		return nil, err
	}
	return e.validateValue(in, rules)
}

// ValidateFieldWithRules evaluates compiled rules on one value. Pass one
// FieldOptions or omit it for defaults. Reuse rules from CompileRules,
// CompileRule, or NewRule.
func (v *Validator) ValidateFieldWithRules(ctx context.Context, value any, rules []Rule, options ...FieldOptions) (FieldErrors, error) {
	e, in, err := v.newFieldValidation(ctx, value, options)
	if err != nil {
		return nil, err
	}
	return e.validateValue(in, rules)
}

func (v *Validator) newFieldValidation(ctx context.Context, value any, options []FieldOptions) (*validationRun, RuleInput, error) {
	opts, err := resolveOptions(options)
	if err != nil {
		return nil, RuleInput{}, err
	}
	e, err := v.newValidationRun(ctx, opts.Locale, opts.PrepareValue)
	if err != nil {
		return nil, RuleInput{}, err
	}
	if opts.Path != "" && !validFieldPath(opts.Path) {
		return nil, RuleInput{}, fmt.Errorf("invalid field path %q", opts.Path)
	}
	e.uniqueExclusions = opts.UniqueExclusions
	return e, RuleInput{Value: value, Path: opts.Path, Label: opts.Label, LookupField: opts.LookupField}, nil
}

func resolveOptions[T any](options []T) (T, error) {
	var zero T
	switch len(options) {
	case 0:
		return zero, nil
	case 1:
		return options[0], nil
	default:
		return zero, fmt.Errorf("validation accepts at most one options value")
	}
}
