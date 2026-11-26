package vali

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
)

func TestPrepareTagsBeforeRules(t *testing.T) {
	type address string
	type row struct {
		Target string   `json:"target" prepare:"trim|lowercase" validate:"same:/email"`
		Tags   []string `json:"tags" prepare:"dive|trim|uppercase" validate:"dive|required|in:GO"`
	}
	// These aliases matter to the rules and must survive preprocessing.
	type aliasForm struct {
		Confirm address   `json:"confirm" prepare:"trim|lowercase" validate:"same:email"`
		Email   address   `json:"email" prepare:"trim|lowercase" validate:"email|unique:users,email"`
		Role    string    `json:"role" prepare:"trim|uppercase"`
		Company string    `json:"company" validate:"required_if:role,ADMIN"`
		Code    **address `json:"code" prepare:"trim|uppercase" validate:"in:ID"`
	}
	var checks []any
	v := newTestValidator(t, Config{DatabaseLookup: DatabaseLookupFunc(func(_ context.Context, query DatabaseQuery) (bool, error) {
		checks = append(checks, query.Value)
		return false, nil
	})})
	code := address(" id ")
	pointer := &code
	input := aliasForm{
		Confirm: " PERSON@EXAMPLE.ORG ", Email: " Person@Example.Org ",
		Role: " admin ", Code: &pointer,
	}
	issues, err := v.ValidateStruct(context.Background(), &input)
	if err != nil || len(issues) != 1 || issues[0].Path != "company" ||
		!reflect.DeepEqual(checks, []any{address("person@example.org")}) {
		t.Fatalf("got %v, %v with checks %v", issues, err, checks)
	}
	if input.Email != " Person@Example.Org " || input.Role != " admin " || code != " id " {
		t.Fatalf("input was mutated: %+v, %q", input, code)
	}

	// Nested absolute comparisons and element transforms also see the prepared copy.
	nested := struct {
		Users []row  `json:"users"`
		Email string `json:"email" prepare:"trim|lowercase"`
	}{Users: []row{{Target: " OTHER@EXAMPLE.ORG ", Tags: []string{" go ", " "}}}, Email: " Other@Example.Org "}
	issues, err = v.ValidateStruct(context.Background(), nested)
	if err != nil || len(issues) != 1 || issues[0].Path != "users.0.tags.1" {
		t.Fatalf("nested got %v, %v", issues, err)
	}
	if nested.Users[0].Target != " OTHER@EXAMPLE.ORG " || nested.Users[0].Tags[0] != " go " {
		t.Fatalf("nested input was mutated: %+v", nested)
	}
}

func TestCustomPrepareHookAndFailures(t *testing.T) {
	type form struct {
		Role string `json:"role" prepare:"default_role:guest|uppercase" validate:"required|in:MEMBER,GUEST"`
	}
	var builds atomic.Int32
	v := newTestValidator(t, Config{PreprocessorFactories: map[string]PreprocessorFactory{
		"default_role": func(args []string) (Preprocessor, error) {
			builds.Add(1)
			if len(args) != 1 {
				return nil, fmt.Errorf("default_role needs one argument")
			}
			return func(_ context.Context, value any) (any, error) {
				if value == "" {
					return args[0], nil
				}
				return value, nil
			}, nil
		},
	}})
	ctx := context.Background()
	for _, opts := range []StructOptions{
		{PrepareForm: func(_ context.Context, value any) (any, error) {
			input := value.(form)
			input.Role = "member"
			return input, nil
		}},
		{},
	} {
		if issues, err := v.ValidateStruct(ctx, form{}, opts); err != nil || len(issues) != 0 {
			t.Fatalf("got %v, %v", issues, err)
		}
	}
	if builds.Load() != 1 {
		t.Fatalf("compiled preprocessor %d times", builds.Load())
	}
	rules := compileTestRules(t, v, "required|email")
	issues, err := v.ValidateFieldWithRules(ctx, " addr@example.org ", rules, FieldOptions{
		PrepareValue: func(_ context.Context, value any) (any, error) {
			return strings.TrimSpace(value.(string)), nil
		},
	})
	if err != nil || len(issues) != 0 {
		t.Fatalf("standalone got %v, %v", issues, err)
	}
	stopped := errors.New("cannot prepare")
	issues, err = v.ValidateStruct(ctx, form{}, StructOptions{PrepareForm: func(context.Context, any) (any, error) {
		return nil, stopped
	}})
	if !errors.Is(err, stopped) || len(issues) != 0 {
		t.Fatalf("hook failure got %v, %v", issues, err)
	}

	type invalid struct {
		Email string `validate:"unique:users,email"`
		Count int    `prepare:"fail"`
	}
	for _, tc := range []struct {
		process Preprocessor
		isError bool
	}{
		{func(context.Context, any) (any, error) { return nil, stopped }, true},
		{func(context.Context, any) (any, error) { return "wrong type", nil }, false},
		{func(context.Context, any) (any, error) { return nil, nil }, false},
	} {
		v := newTestValidator(t, Config{
			PreprocessorFactories: map[string]PreprocessorFactory{"fail": func([]string) (Preprocessor, error) {
				return tc.process, nil
			}},
			DatabaseLookup: DatabaseLookupFunc(func(context.Context, DatabaseQuery) (bool, error) {
				t.Error("validation ran before preprocessing completed")
				return false, nil
			}),
		})
		issues, err := v.ValidateStruct(ctx, invalid{Email: "addr@example.org"})
		if err == nil || !strings.Contains(err.Error(), "Count") || errors.Is(err, stopped) != tc.isError || len(issues) != 0 {
			t.Fatalf("preprocessor failure got %v, %v", issues, err)
		}
	}
}

func TestPrepareWithCyclesAndSharedNodes(t *testing.T) {
	type node struct {
		Name     string `json:"name" prepare:"trim|uppercase" validate:"required|in:OK"`
		Next     *node  `json:"next"`
		Children []node `json:"children"`
	}
	root := &node{Name: " ok ", Children: make([]node, 1)}
	root.Next = root
	root.Children[0].Children = root.Children
	type form struct {
		Left  *node `json:"left"`
		Right *node `json:"right"`
	}
	v := newTestValidator(t, Config{})
	issues, err := v.ValidateStruct(context.Background(), &form{root, root})
	if err != nil || len(issues) != 2 ||
		issues[0].Path != "left.children.0.name" || issues[1].Path != "right.children.0.name" {
		t.Fatalf("got %v, %v", issues, err)
	}
	if root.Name != " ok " || root.Next != root {
		t.Fatalf("cycle or data changed: %+v", root)
	}
}

func TestPrepareSchemaErrorsAndNilValues(t *testing.T) {
	v := newTestValidator(t, Config{})
	type ignored struct {
		Bad int `prepare:"unknown"`
	}
	form := struct {
		Empty      *string `prepare:"trim" validate:"nullable|email"`
		Skipped    ignored `validate:"-"`
		SkippedTag string  `prepare:"-" validate:"nullable|string"`
	}{}
	if issues, err := v.ValidateStruct(context.Background(), form); err != nil || len(issues) != 0 {
		t.Fatalf("got %v, %v", issues, err)
	}
	for _, invalid := range []any{
		struct{ Unknown *ignored }{},
		struct {
			Count int `prepare:"trim"`
		}{},
		struct {
			Values []string `prepare:"dive"`
		}{},
		struct {
			Values []string `prepare:"dive:arg|trim"`
		}{},
		struct {
			Text string `prepare:"trim:arg"`
		}{},
		struct {
			Text string `prepare:"dive|trim"`
		}{},
		struct {
			Text any `prepare:"trim"`
		}{42},
	} {
		if issues, err := v.ValidateStruct(context.Background(), invalid); err == nil || len(issues) > 0 {
			t.Errorf("%T got %v, %v; expected preprocessing error", invalid, issues, err)
		}
	}
}
