// Package vali validates Go forms using reusable rules and localized errors.
// Use ValidateStruct for a tagged struct or ValidateField for a rule expression.
// Create a Validator with New to configure custom rules, catalogs, or database
// lookups. Validation returns field failures separately from execution errors.
package vali

import (
	"fmt"
	"regexp"
	"sync"
)

// Config supplies the catalogs, factories, and database lookup for New.
type Config struct {
	// FallbackLocale is tried after the explicit or negotiated locale.
	FallbackLocale string
	// Catalogs extends or overrides the built-in translations by locale.
	Catalogs map[string]Catalog
	// RuleFactories registers rule factories by name for tags and rule expressions.
	RuleFactories map[string]RuleFactory
	// PreprocessorFactories registers factories by name for prepare tags.
	PreprocessorFactories map[string]PreprocessorFactory
	// DatabaseLookup supplies database lookups for unique and exists rules.
	DatabaseLookup DatabaseLookup
}

// FormAliasProvider lets a root struct select form-specific catalog messages.
// ValidationFormAlias must return the type's alias even on a zero-value form.
// An empty alias selects the shared catalog messages.
type FormAliasProvider interface {
	ValidationFormAlias() string
}

// Validator compiles and evaluates rules for structs and individual values.
// Construct a Validator with New. A Validator is safe for concurrent use.
type Validator struct {
	ruleFactories         map[string]RuleFactory
	preprocessorFactories map[string]PreprocessorFactory
	catalogs              map[string]compiledCatalog
	localeNames           []string
	fallbackLocale        string
	databaseLookup        DatabaseLookup
	schemaCache           sync.Map
}

var ruleNameSyntax = regexp.MustCompile("^[a-z][a-z0-9_]*$")

// New constructs a validator with built-in English and Simplified Chinese.
func New(config Config) (*Validator, error) {
	catalogs, err := compileCatalogs(config.Catalogs)
	if err != nil {
		return nil, err
	}
	fallback := normalizeLocale(config.FallbackLocale)
	if fallback != "" && !localeSyntax.MatchString(fallback) {
		return nil, fmt.Errorf("invalid fallback locale %q", fallback)
	}
	v := &Validator{
		ruleFactories:         make(map[string]RuleFactory, len(config.RuleFactories)),
		preprocessorFactories: make(map[string]PreprocessorFactory, len(config.PreprocessorFactories)),
		catalogs:              catalogs,
		localeNames:           sortedLocaleNames(catalogs),
		fallbackLocale:        fallback,
		databaseLookup:        config.DatabaseLookup,
	}
	for name, factory := range config.RuleFactories {
		if err := validateCustomRuleName(name); err != nil {
			return nil, err
		}
		if factory == nil {
			return nil, fmt.Errorf("invalid custom rule %q", name)
		}
		v.ruleFactories[name] = factory
	}
	for name, factory := range config.PreprocessorFactories {
		if !ruleNameSyntax.MatchString(name) || factory == nil || name == "dive" || textPreprocessors[name] != nil {
			return nil, fmt.Errorf("invalid or reserved preprocessor name %q", name)
		}
		v.preprocessorFactories[name] = factory
	}
	return v, nil
}
