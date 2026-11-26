package vali

import (
	"context"
	"fmt"
	"reflect"
	"regexp"
	"strings"
)

// RowExclusion omits records whose Column matches Value in a uniqueness check.
type RowExclusion struct {
	Column string
	Value  any
}

// DatabaseQuery describes a database presence check.
type DatabaseQuery struct {
	Table     string
	Column    string
	Value     any
	Exclusion *RowExclusion
}

// DatabaseLookup reports whether a matching record exists. Implementations
// shared by multiple requests must support concurrent calls.
type DatabaseLookup interface {
	Lookup(ctx context.Context, query DatabaseQuery) (bool, error)
}

// DatabaseLookupFunc adapts a function to DatabaseLookup.
type DatabaseLookupFunc func(context.Context, DatabaseQuery) (bool, error)

func (f DatabaseLookupFunc) Lookup(ctx context.Context, query DatabaseQuery) (bool, error) {
	return f(ctx, query)
}

var identifierSyntax = regexp.MustCompile("^[a-zA-Z_][a-zA-Z0-9_]*$")

func validIdentifier(identifier string) bool { return identifierSyntax.MatchString(identifier) }

func validTable(table string) bool {
	first, second, qualified := strings.Cut(table, ".")
	return validIdentifier(first) && (!qualified || validIdentifier(second))
}

func validateRowExclusion(exclusion *RowExclusion) error {
	if exclusion != nil && (!validIdentifier(exclusion.Column) || unpack(exclusion.Value) == nil) {
		return fmt.Errorf("unique exclusion requires a valid column and a non-nil value")
	}
	return nil
}

func compileDatabaseRule(rule Rule, args []string) (Rule, error) {
	table, column := args[0], args[1]
	if err := validateLookupTarget(table, column); err != nil {
		return Rule{}, err
	}
	rule.check = func(e *validationRun, in RuleInput, _ bool) (*Violation, error) {
		query := DatabaseQuery{Table: table, Column: column, Value: in.normalized}
		if rule.name == "unique" {
			if exclusion, ok := e.uniqueExclusions[in.Path]; ok {
				query.Exclusion = &exclusion
			}
		}
		return checkDatabase(e.ctx, e.v.databaseLookup, rule.name, query)
	}
	return rule, nil
}

func validateLookupTarget(table, column string) error {
	if !validTable(table) || !validIdentifier(column) {
		return fmt.Errorf("expected a valid table and column")
	}
	return nil
}

func checkDatabase(ctx context.Context, lookup DatabaseLookup, name string, query DatabaseQuery) (*Violation, error) {
	if isNilValue(reflect.ValueOf(lookup)) {
		return nil, fmt.Errorf("no database lookup configured")
	}
	if query.Value == nil {
		return &Violation{}, nil
	}
	if err := validateRowExclusion(query.Exclusion); err != nil {
		return nil, err
	}
	found, err := lookup.Lookup(ctx, query)
	if err != nil {
		return nil, err
	}
	if found == (name == "exists") {
		return nil, nil
	}
	return &Violation{}, nil
}
