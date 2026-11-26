package vali

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"sync"
)

type schema struct {
	root           *structPlan
	plans          map[reflect.Type]*structPlan
	hasPreparation bool
	formAlias      string
}

type structPlan struct {
	fields []*fieldPlan
	byName map[string]*fieldPlan
}

type fieldPlan struct {
	index   int
	typ     reflect.Type
	name    string
	label   string
	chain   *ruleChain
	prepare *prepareChain
	descend bool
	routes  map[string]fieldRoute
}

type schemaCacheEntry struct {
	once sync.Once
	plan *schema
	err  error
}

func (v *Validator) schemaFor(typ reflect.Type) (*schema, error) {
	cached, ok := v.schemaCache.Load(typ)
	if !ok {
		cached, _ = v.schemaCache.LoadOrStore(typ, &schemaCacheEntry{})
	}
	entry := cached.(*schemaCacheEntry)
	entry.once.Do(func() { entry.plan, entry.err = v.compileSchema(typ) })
	return entry.plan, entry.err
}

func (v *Validator) compileSchema(typ reflect.Type) (*schema, error) {
	model := &schema{plans: make(map[reflect.Type]*structPlan)}
	var order []reflect.Type
	if err := v.compileStruct(model, typ, &order); err != nil {
		return nil, err
	}
	model.root = model.plans[typ]
	if reflect.PointerTo(typ).Implements(reflect.TypeFor[FormAliasProvider]()) {
		model.formAlias = reflect.New(typ).Interface().(FormAliasProvider).ValidationFormAlias()
	}
	for _, current := range order {
		plan := model.plans[current]
		for _, field := range plan.fields {
			routes, err := model.relatedRoutes(plan, field.chain)
			if err != nil {
				return nil, fmt.Errorf("%s.%s: %w", current, field.name, err)
			}
			field.routes = routes
		}
	}
	return model, nil
}

func (v *Validator) compileStruct(model *schema, typ reflect.Type, order *[]reflect.Type) error {
	if model.plans[typ] != nil {
		return nil
	}
	plan := &structPlan{byName: make(map[string]*fieldPlan)}
	model.plans[typ] = plan
	*order = append(*order, typ)
	for i := 0; i < typ.NumField(); i++ {
		info := typ.Field(i)
		if !info.IsExported() {
			continue
		}
		field, nested, err := v.compileField(i, info)
		if err != nil {
			return fmt.Errorf("%s.%s: %w", typ, info.Name, err)
		}
		if field == nil {
			continue
		}
		if _, exists := plan.byName[field.name]; exists {
			return fmt.Errorf("%s: duplicate field path %q", typ, field.name)
		}
		plan.fields = append(plan.fields, field)
		plan.byName[field.name] = field
		model.hasPreparation = model.hasPreparation || field.prepare != nil
		if nested != nil {
			if err := v.compileStruct(model, nested, order); err != nil {
				return err
			}
		}
	}
	return nil
}

func (v *Validator) compileField(index int, info reflect.StructField) (*fieldPlan, reflect.Type, error) {
	tags, err := parseStructTags(string(info.Tag))
	if err != nil {
		return nil, nil, err
	}
	jsonName, _, _ := strings.Cut(tags["json"], ",")
	raw, tagged := tags["validate"]
	if jsonName == "-" || raw == "-" {
		return nil, nil, nil
	}
	name := jsonName
	if name == "" {
		name = info.Name
	}
	if !validPathSegment(name) {
		return nil, nil, fmt.Errorf("invalid field path segment %q", name)
	}
	nested := nestedStructType(info.Type)
	field := &fieldPlan{index: index, typ: info.Type, name: name, label: tags["label"], descend: nested != nil}
	if preparation, ok := tags["prepare"]; ok && preparation != "-" {
		field.prepare, err = v.compilePreparation(info.Type, preparation)
		if err != nil {
			return nil, nil, fmt.Errorf("prepare: %w", err)
		}
	}
	if tagged {
		rules, err := v.CompileRules(raw)
		if err != nil {
			return nil, nil, err
		}
		field.chain, err = compileRuleChain(rules)
		if err != nil {
			return nil, nil, err
		}
		if err := validateDiveType(field.typ, field.chain); err != nil {
			return nil, nil, err
		}
	}
	return field, nested, nil
}

func validateDiveType(typ reflect.Type, chain *ruleChain) error {
	for node := chain; node.elements != nil; node = node.elements {
		var ok bool
		typ, ok = collectionElementType(typ)
		if !ok {
			return fmt.Errorf("dive requires an array or slice")
		}
	}
	return nil
}

func collectionElementType(typ reflect.Type) (reflect.Type, bool) {
	typ = indirectType(typ)
	if typ != nil {
		switch typ.Kind() {
		case reflect.Interface:
			return typ, true // Inspect the concrete value at execution time.
		case reflect.Slice, reflect.Array:
			return typ.Elem(), true
		}
	}
	return nil, false
}

type routeStep struct {
	index int
	field *fieldPlan
}

type fieldRoute struct {
	absolute bool
	path     string
	steps    []routeStep
	label    string
	scope    *structPlan
	typ      reflect.Type
}

func (m *schema) relatedRoutes(scope *structPlan, chain *ruleChain) (map[string]fieldRoute, error) {
	var routes map[string]fieldRoute
	for node := chain; node != nil; node = node.elements {
		for _, rule := range node.checks {
			for _, path := range rule.refs {
				if _, ok := routes[path]; ok {
					continue
				}
				route, ok := m.route(scope, path)
				if !ok {
					return nil, fmt.Errorf("%s: unknown related field %q", rule.name, path)
				}
				if routes == nil {
					routes = make(map[string]fieldRoute)
				}
				routes[path] = route
			}
		}
	}
	return routes, nil
}

func (m *schema) route(scope *structPlan, path string) (fieldRoute, bool) {
	localPath, absolute := strings.CutPrefix(path, "/")
	if !validFieldPath(localPath) {
		return fieldRoute{}, false
	}
	if absolute {
		scope = m.root
	}
	result, ok := m.findRoute(scope, localPath, false)
	result.absolute = absolute
	return result, ok
}

func (m *schema) pathPattern(path string) string {
	route, ok := m.findRoute(m.root, path, false)
	if !ok {
		return ""
	}
	parts := strings.Split(path, ".")
	for i, step := range route.steps {
		if step.field == nil {
			parts[i] = "*"
		}
	}
	return strings.Join(parts, ".")
}

func (m *schema) findRoute(scope *structPlan, path string, allowWildcard bool) (fieldRoute, bool) {
	result := fieldRoute{path: path}
	var typ reflect.Type
	for _, segment := range strings.Split(path, ".") {
		if typ != nil {
			typ = indirectType(typ)
			if typ == nil {
				return fieldRoute{}, false
			}
			switch typ.Kind() {
			case reflect.Slice, reflect.Array:
				if allowWildcard && segment == "*" {
					typ = typ.Elem()
					continue
				}
				index, err := strconv.Atoi(segment)
				if err != nil || index < 0 || strconv.Itoa(index) != segment ||
					(typ.Kind() == reflect.Array && index >= typ.Len()) {
					return fieldRoute{}, false
				}
				result.steps = append(result.steps, routeStep{index: index})
				typ = typ.Elem()
				continue
			case reflect.Struct:
				scope = m.plans[typ]
			default:
				return fieldRoute{}, false
			}
		}
		if scope == nil || scope.byName[segment] == nil {
			return fieldRoute{}, false
		}
		field := scope.byName[segment]
		result.steps = append(result.steps, routeStep{index: field.index, field: field})
		result.label, typ = field.label, field.typ
	}
	result.scope, result.typ = scope, typ
	return result, true
}

func (r fieldRoute) resolve(value reflect.Value, prefix string) RelatedField {
	result := RelatedField{Path: joinPath(prefix, r.path), Label: r.label}
	for _, step := range r.steps {
		for value.Kind() == reflect.Ptr {
			value = value.Elem()
		}
		if !value.IsValid() {
			return result
		}
		if step.field != nil {
			value = value.Field(step.index)
		} else {
			if step.index >= value.Len() {
				return result
			}
			value = value.Index(step.index)
		}
	}
	result.Value = value.Interface()
	return result
}

func (e *validationRun) walkStruct(value reflect.Value, path, pattern string, targets ruleTargets) error {
	plan := e.schema.plans[value.Type()]
	for _, field := range plan.fields {
		children := targets.children(field.name, false)
		if field.chain == nil && !field.descend && len(children) == 0 {
			continue
		}
		current := value.Field(field.index)
		input := RuleInput{
			Path: joinPath(path, field.name), Label: field.label, Value: current.Interface(),
			name: field.name, pattern: joinPath(pattern, field.name),
			LookupField: func(reference string) (RelatedField, bool) {
				route, ok := field.routes[reference]
				if !ok {
					route, ok = e.schema.route(plan, reference)
					if !ok {
						return RelatedField{}, false
					}
				}
				if route.absolute {
					return route.resolve(e.root, ""), true
				}
				return route.resolve(value, path), true
			},
		}
		if err := e.walk(current, input, field.chain, field.descend, children); err != nil {
			return err
		}
	}
	return nil
}
