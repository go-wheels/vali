package vali

import (
	"context"
	"errors"
	"fmt"
)

// ErrInvalidValue identifies a value that failed a standalone validation.
// Invalid arguments and lookup failures return their own errors.
var ErrInvalidValue = errors.New("invalid value")

// Required checks that value is present and non-empty.
func Required(value any) (bool, error) {
	return checkStandaloneValue("required", value)
}

// String checks that value is a string.
func String(value any) (bool, error) {
	return checkStandaloneValue("string", value)
}

// Boolean checks that value is a boolean or a supported 0 or 1.
func Boolean(value any) (bool, error) {
	return checkStandaloneValue("boolean", value)
}

// Array checks that value is an array or a non-nil slice.
func Array(value any) (bool, error) {
	return checkStandaloneValue("array", value)
}

// Numeric checks that value is a finite number or a valid decimal string.
func Numeric(value any) (bool, error) {
	return checkStandaloneValue("numeric", value)
}

// Integer checks that value is an integer or a valid integer string.
func Integer(value any) (bool, error) {
	return checkStandaloneValue("integer", value)
}

// Email checks the format of an email address.
func Email(value any) (bool, error) {
	return checkStandaloneValue("email", value)
}

// URL checks that value is an absolute HTTP or HTTPS URL with a host.
func URL(value any) (bool, error) {
	return checkStandaloneValue("url", value)
}

// CNMobile checks the 11-digit domestic format of a mainland China mobile number.
func CNMobile(value any) (bool, error) {
	return checkStandaloneValue("cn_mobile", value)
}

// CNIDCard checks the format, birth date, and checksum of an 18-character
// mainland China resident identity number.
func CNIDCard(value any) (bool, error) {
	return checkStandaloneValue("cn_id_card", value)
}

// Min checks the numeric value, string rune count, or collection length
// against an inclusive minimum given as a decimal string.
func Min(value any, min string) (bool, error) {
	return checkStandaloneValue("min", value, min)
}

// Max checks the numeric value, string rune count, or collection length
// against an inclusive maximum given as a decimal string.
func Max(value any, max string) (bool, error) {
	return checkStandaloneValue("max", value, max)
}

// Between checks the numeric value, string rune count, or collection length
// against inclusive bounds given as decimal strings.
func Between(value any, min, max string) (bool, error) {
	return checkStandaloneValue("between", value, min, max)
}

// Size checks the numeric value, string rune count, or collection length
// against an exact size given as a decimal string.
func Size(value any, size string) (bool, error) {
	return checkStandaloneValue("size", value, size)
}

// In checks that the scalar value matches one of the choices.
func In(value any, choices ...string) (bool, error) {
	return checkStandaloneValue("in", value, choices...)
}

// NotIn checks that the scalar value is absent from the choices.
func NotIn(value any, choices ...string) (bool, error) {
	return checkStandaloneValue("not_in", value, choices...)
}

// Regex checks that a string matches the regular expression.
func Regex(value any, pattern string) (bool, error) {
	return checkStandaloneValue("regex", value, pattern)
}

// Same checks that other is present and deeply equal to value.
func Same(value, other any) (bool, error) {
	return checkStandaloneRelated("same", value, []any{other}, nil)
}

// Different checks that other is present and differs from value.
func Different(value, other any) (bool, error) {
	return checkStandaloneRelated("different", value, []any{other}, nil)
}

// Confirmed checks that confirmation is present and deeply equal to value.
func Confirmed(value, confirmation any) (bool, error) {
	return checkStandaloneRelated("confirmed", value, []any{confirmation}, nil)
}

// RequiredIf requires value when other matches any of the given choices.
func RequiredIf(value, other any, choice string, more ...string) (bool, error) {
	return checkStandaloneRelated("required_if", value, []any{other}, append([]string{choice}, more...))
}

// RequiredUnless requires value when other matches none of the given choices.
func RequiredUnless(value, other any, choice string, more ...string) (bool, error) {
	return checkStandaloneRelated("required_unless", value, []any{other}, append([]string{choice}, more...))
}

// RequiredWith requires value when any related value is present and non-empty.
func RequiredWith(value, other any, more ...any) (bool, error) {
	return checkStandaloneRelated("required_with", value, append([]any{other}, more...), nil)
}

// RequiredWithout requires value when any related value is missing or empty.
func RequiredWithout(value, other any, more ...any) (bool, error) {
	return checkStandaloneRelated("required_without", value, append([]any{other}, more...), nil)
}

// Unique checks that no matching database record exists. An exclusion, when
// supplied, identifies a record to ignore.
func Unique(ctx context.Context, lookup DatabaseLookup, value any, table, column string, exclusion *RowExclusion) (bool, error) {
	return checkStandaloneDatabase(ctx, lookup, "unique", value, table, column, exclusion)
}

// Exists checks that a matching database record exists.
func Exists(ctx context.Context, lookup DatabaseLookup, value any, table, column string) (bool, error) {
	return checkStandaloneDatabase(ctx, lookup, "exists", value, table, column, nil)
}

func checkStandaloneValue(name string, value any, args ...string) (bool, error) {
	rule, err := compileBuiltinRule(name, args)
	if err != nil {
		return standaloneResult(name, nil, err)
	}
	violation, err := rule.check(nil, RuleInput{normalized: unpack(value)}, false)
	return standaloneResult(name, violation, err)
}

func checkStandaloneRelated(name string, value any, others []any, choices []string) (bool, error) {
	fields := make([]RelatedField, len(others))
	for i, other := range others {
		fields[i].Value = unpack(other)
	}
	return standaloneResult(name, checkRelatedFields(name, unpack(value), fields, choices), nil)
}

func checkStandaloneDatabase(ctx context.Context, lookup DatabaseLookup, name string, value any, table, column string, exclusion *RowExclusion) (bool, error) {
	if ctx == nil {
		return false, fmt.Errorf("%s: validation requires a non-nil context", name)
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if err := validateLookupTarget(table, column); err != nil {
		return standaloneResult(name, nil, err)
	}
	query := DatabaseQuery{Table: table, Column: column, Value: unpack(value), Exclusion: exclusion}
	violation, err := checkDatabase(ctx, lookup, name, query)
	if err == nil {
		err = ctx.Err()
	}
	return standaloneResult(name, violation, err)
}

func standaloneResult(name string, violation *Violation, err error) (bool, error) {
	if err != nil {
		return false, fmt.Errorf("%s: %w", name, err)
	}
	if violation != nil {
		return false, fmt.Errorf("%s: %w", name, ErrInvalidValue)
	}
	return true, nil
}
