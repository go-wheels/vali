package vali

import (
	"context"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"unsafe"
)

// Preprocessor transforms input before validation. It must not mutate or
// retain the input. A preprocessor used in a prepare tag must return a value
// assignable to the field's type.
type Preprocessor func(context.Context, any) (any, error)

// PreprocessorFactory builds a reusable preprocessor from decoded tag arguments.
// Factories and their returned functions must be safe for concurrent use.
type PreprocessorFactory func(args []string) (Preprocessor, error)

var textPreprocessors = map[string]func(string) string{
	"trim":      strings.TrimSpace,
	"lowercase": strings.ToLower,
	"uppercase": strings.ToUpper,
}

type prepareStep struct {
	name    string
	process Preprocessor
}

type prepareChain struct {
	steps    []prepareStep
	elements *prepareChain
}

func (v *Validator) compilePreparation(typ reflect.Type, raw string) (*prepareChain, error) {
	tags, err := parseExpression(raw)
	if err != nil {
		return nil, err
	}
	root := &prepareChain{}
	node := root
	for _, tag := range tags {
		if tag.name == "dive" {
			if len(tag.args) != 0 {
				return nil, fmt.Errorf("dive takes no arguments")
			}
			var ok bool
			typ, ok = collectionElementType(typ)
			if !ok {
				return nil, fmt.Errorf("dive requires an array or slice")
			}
			node.elements = &prepareChain{}
			node = node.elements
			continue
		}
		step, err := v.compilePrepareStep(typ, tag)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", tag.name, err)
		}
		node.steps = append(node.steps, step)
	}
	if len(node.steps) == 0 {
		return nil, fmt.Errorf("dive must be followed by a preprocessor")
	}
	return root, nil
}

func (v *Validator) compilePrepareStep(typ reflect.Type, tag expressionPart) (prepareStep, error) {
	var process Preprocessor
	if transform := textPreprocessors[tag.name]; transform != nil {
		if len(tag.args) != 0 {
			return prepareStep{}, fmt.Errorf("takes no arguments")
		}
		leaf := indirectType(typ)
		if leaf == nil || (leaf.Kind() != reflect.String && leaf.Kind() != reflect.Interface) {
			return prepareStep{}, fmt.Errorf("requires a string")
		}
		process = textPreprocessor(transform)
	} else if factory := v.preprocessorFactories[tag.name]; factory != nil {
		var err error
		process, err = factory(tag.args)
		if err != nil {
			return prepareStep{}, err
		}
		if process == nil {
			return prepareStep{}, fmt.Errorf("factory returned a nil preprocessor")
		}
	} else {
		return prepareStep{}, fmt.Errorf("unknown preprocessor")
	}
	return prepareStep{name: tag.name, process: process}, nil
}

func textPreprocessor(transform func(string) string) Preprocessor {
	return func(_ context.Context, input any) (any, error) {
		value := reflect.ValueOf(input)
		leaf := indirectValue(value)
		if !leaf.IsValid() {
			return input, nil
		}
		if leaf.Kind() != reflect.String {
			return nil, fmt.Errorf("requires a string, got %s", leaf.Type())
		}
		text := transform(leaf.String())
		if text == leaf.String() {
			return input, nil
		}
		return replacePreparedText(value, text).Interface(), nil
	}
}

func replacePreparedText(value reflect.Value, text string) reflect.Value {
	switch value.Kind() {
	case reflect.Ptr:
		result := reflect.New(value.Type().Elem()).Convert(value.Type())
		result.Elem().Set(replacePreparedText(value.Elem(), text))
		return result
	case reflect.Interface:
		result := reflect.New(value.Type()).Elem()
		result.Set(replacePreparedText(value.Elem(), text))
		return result
	default:
		result := reflect.New(value.Type()).Elem()
		result.SetString(text)
		return result
	}
}

type preparedValueReference struct {
	typ      reflect.Type
	source   unsafe.Pointer
	length   int
	elements *prepareChain
	descend  bool
}

type formPreparation struct {
	ctx    context.Context
	schema *schema
	copies map[preparedValueReference]reflect.Value
}

func (p *formPreparation) apply(value reflect.Value, path string, chain *prepareChain, descend bool) (reflect.Value, error) {
	if err := p.ctx.Err(); err != nil {
		return reflect.Value{}, err
	}
	var elements *prepareChain
	if chain != nil {
		for _, step := range chain.steps {
			if err := p.ctx.Err(); err != nil {
				return reflect.Value{}, err
			}
			next, err := step.process(p.ctx, value.Interface())
			if err != nil {
				return reflect.Value{}, fmt.Errorf("prepare %s (%s): %w", path, step.name, err)
			}
			replacement := reflect.ValueOf(next)
			if !replacement.IsValid() && canBeNil(value.Kind()) {
				replacement = reflect.Zero(value.Type())
			}
			if !replacement.IsValid() || !replacement.Type().AssignableTo(value.Type()) {
				return reflect.Value{}, fmt.Errorf("prepare %s (%s): result type %T is not assignable to %s", path, step.name, next, value.Type())
			}
			if replacement.Type() != value.Type() {
				boxed := reflect.New(value.Type()).Elem()
				boxed.Set(replacement)
				value = boxed
			} else {
				value = replacement
			}
		}
		elements = chain.elements
	}
	return p.copyChildren(value, path, elements, descend)
}

func (p *formPreparation) copyChildren(value reflect.Value, path string, elements *prepareChain, descend bool) (reflect.Value, error) {
	if (!descend && elements == nil) || isNilValue(value) {
		return value, nil
	}
	switch value.Kind() {
	case reflect.Ptr, reflect.Slice:
		return p.copyReference(value, path, elements, descend)
	case reflect.Interface:
		inner, err := p.copyChildren(value.Elem(), path, elements, descend)
		if err != nil {
			return reflect.Value{}, err
		}
		result := reflect.New(value.Type()).Elem()
		result.Set(inner)
		return result, nil
	case reflect.Array:
		result := reflect.New(value.Type()).Elem()
		return result, p.copyItems(result, value, path, elements, descend)
	case reflect.Struct:
		if elements == nil {
			return p.copyStruct(value, path)
		}
	}
	return reflect.Value{}, fmt.Errorf("prepare %s: dive requires an array or slice", path)
}

func (p *formPreparation) copyReference(value reflect.Value, path string, elements *prepareChain, descend bool) (reflect.Value, error) {
	key := preparedValueReference{
		typ: value.Type(), source: value.UnsafePointer(),
		elements: elements, descend: descend,
	}
	if value.Kind() == reflect.Slice {
		key.length = value.Len()
	}
	if copy, ok := p.copies[key]; ok {
		return copy, nil
	}
	// Publish the copy before visiting children so back-references point to it.
	if value.Kind() == reflect.Slice {
		result := reflect.MakeSlice(value.Type(), value.Len(), value.Len())
		p.copies[key] = result
		return result, p.copyItems(result, value, path, elements, descend)
	}
	result := reflect.New(value.Type().Elem()).Convert(value.Type())
	p.copies[key] = result
	child, err := p.copyChildren(value.Elem(), path, elements, descend)
	if err != nil {
		return reflect.Value{}, err
	}
	result.Elem().Set(child)
	return result, nil
}

func (p *formPreparation) copyItems(dst, src reflect.Value, path string, elements *prepareChain, descend bool) error {
	for i := 0; i < src.Len(); i++ {
		item, err := p.apply(src.Index(i), joinPath(path, strconv.Itoa(i)), elements, descend)
		if err != nil {
			return err
		}
		dst.Index(i).Set(item)
	}
	return nil
}

func (p *formPreparation) copyStruct(value reflect.Value, path string) (reflect.Value, error) {
	result := reflect.New(value.Type()).Elem()
	result.Set(value)
	for _, field := range p.schema.plans[value.Type()].fields {
		if field.prepare == nil && !field.descend {
			continue
		}
		prepared, err := p.apply(value.Field(field.index), joinPath(path, field.name), field.prepare, field.descend)
		if err != nil {
			return reflect.Value{}, err
		}
		result.Field(field.index).Set(prepared)
	}
	return result, nil
}
