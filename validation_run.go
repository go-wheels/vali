package vali

import (
	"context"
	"fmt"
	"reflect"
	"strconv"
	"strings"
)

func (e *validationRun) validateValue(in RuleInput, rules []Rule) (FieldErrors, error) {
	chain, err := compileRuleChain(rules)
	if err != nil {
		return nil, err
	}
	in.Value, err = e.prepareInput(in.Value)
	if err != nil {
		return nil, err
	}
	if in.Value != nil {
		if err := validateDiveType(reflect.TypeOf(in.Value), chain); err != nil {
			return nil, err
		}
	}
	if in.Path == "" {
		in.Path = "value"
	}
	in.name = leafName(in.Path)
	in.pattern = wildcardIndexPath(in.Path)
	if err := e.walk(reflect.ValueOf(in.Value), in, chain, false, nil); err != nil {
		return nil, err
	}
	return e.result()
}

type validationRun struct {
	ctx              context.Context
	prepare          Preprocessor
	uniqueExclusions map[string]RowExclusion
	v                *Validator
	locales          []string
	formAlias        string
	failures         FieldErrors
	active           map[valueReference]bool
	schema           *schema
	root             reflect.Value
}

func (v *Validator) newValidationRun(ctx context.Context, locale string, prepare Preprocessor) (*validationRun, error) {
	if ctx == nil {
		return nil, fmt.Errorf("validation requires a non-nil context")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(locale) == "" {
		header, _ := ctx.Value(acceptLanguageKey{}).(string)
		locale = v.negotiateLocale(header)
	}
	return &validationRun{ctx: ctx, prepare: prepare, v: v, locales: v.localeFallbacks(locale)}, nil
}

func (e *validationRun) prepareInput(value any) (any, error) {
	if e.prepare == nil {
		return value, nil
	}
	prepared, err := e.prepare(e.ctx, value)
	if err != nil {
		return nil, fmt.Errorf("prepare input: %w", err)
	}
	return prepared, e.ctx.Err()
}

func (e *validationRun) result() (FieldErrors, error) {
	if err := e.ctx.Err(); err != nil {
		return nil, err
	}
	return e.failures, nil
}

type ruleChain struct {
	checks                  []Rule
	bail, nullable, numeric bool
	elements                *ruleChain
}

func compileRuleChain(rules []Rule) (*ruleChain, error) {
	chain := &ruleChain{}
	node := chain
	for _, rule := range rules {
		switch rule.name {
		case "dive":
			node.elements = &ruleChain{}
			node = node.elements
		case "bail":
			node.bail = true
		case "nullable":
			node.nullable = true
		default:
			if rule.check == nil {
				return nil, fmt.Errorf("uncompiled rule %q", rule.name)
			}
			node.checks = append(node.checks, rule)
			node.numeric = node.numeric || rule.name == "numeric" || rule.name == "integer"
		}
	}
	if node != chain && len(node.checks) == 0 && !node.bail && !node.nullable {
		return nil, fmt.Errorf("dive must be followed by element rules")
	}
	return chain, nil
}

func (e *validationRun) check(in RuleInput, chain *ruleChain) error {
	if chain == nil || len(chain.checks) == 0 {
		return nil
	}
	in.normalized = unpack(in.Value)
	skip := blankString(in.normalized) || (in.normalized == nil && chain.nullable)
	for _, rule := range chain.checks {
		if err := e.ctx.Err(); err != nil {
			return err
		}
		if skip && !rule.implicit {
			continue
		}
		violation, err := rule.check(e, in, chain.numeric)
		if err != nil {
			return fmt.Errorf("%s (%s): %w", in.Path, rule.name, err)
		}
		if violation != nil {
			e.failures = append(e.failures, e.render(in, rule, violation))
			if chain.bail {
				break
			}
		}
	}
	return nil
}

func (e *validationRun) related(in RuleInput, path string) (RelatedField, error) {
	if in.LookupField != nil {
		if field, ok := in.LookupField(path); ok {
			return field, nil
		}
	}
	return RelatedField{}, fmt.Errorf("unknown related field %q", path)
}

func (e *validationRun) walk(value reflect.Value, in RuleInput, chain *ruleChain, descend bool, targets ruleTargets) error {
	if err := e.ctx.Err(); err != nil {
		return err
	}
	for _, target := range targets {
		chain = appendRuleChain(chain, target.chain)
	}
	if err := e.check(in, chain); err != nil {
		return err
	}
	var elements *ruleChain
	if chain != nil {
		elements = chain.elements
	}
	if !descend && elements == nil && !targets.hasChildren() {
		return nil
	}

	var entered []valueReference
	defer func() {
		for _, ref := range entered {
			delete(e.active, ref)
		}
	}()
	for value.IsValid() {
		kind := value.Kind()
		if kind == reflect.Interface {
			value = value.Elem()
			continue
		}
		if kind == reflect.Ptr || kind == reflect.Slice {
			if value.IsNil() {
				return nil
			}
			ref := valueReference{typ: value.Type(), ptr: value.Pointer()}
			if kind == reflect.Slice {
				ref.len = value.Len()
			}
			if e.active[ref] {
				return nil
			}
			if e.active == nil {
				e.active = make(map[valueReference]bool)
			}
			e.active[ref] = true
			entered = append(entered, ref)
		}
		if kind != reflect.Ptr {
			break
		}
		value = value.Elem()
	}
	switch value.Kind() {
	case reflect.Struct:
		if elements != nil {
			return fmt.Errorf("%s: dive requires an array or slice", in.Path)
		}
		if descend {
			return e.walkStruct(value, in.Path, in.pattern, targets)
		}
	case reflect.Array, reflect.Slice:
		for i := 0; i < value.Len(); i++ {
			item := value.Index(i)
			child := in
			index := strconv.Itoa(i)
			child.Path = joinPath(in.Path, index)
			child.pattern = joinPath(in.pattern, "*")
			child.Value = item.Interface()
			if err := e.walk(item, child, elements, descend, targets.children(index, true)); err != nil {
				return err
			}
		}
	default:
		if elements != nil && value.IsValid() {
			return fmt.Errorf("%s: dive requires an array or slice", in.Path)
		}
	}
	return nil
}
