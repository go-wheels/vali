package vali

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
)

func TestExtraRulesAcceptUnregisteredChecks(t *testing.T) {
	type form struct {
		Role  string `json:"role" label:"角色" prepare:"trim|lowercase" validate:"bail|required"`
		Group string `json:"group" prepare:"trim|lowercase"`
	}
	ctx := context.Background()
	v := newTestValidator(t, Config{Catalogs: map[string]Catalog{
		"zh-CN": {Messages: map[string]string{"temporary_role": ":group 不允许使用此 :attribute。"}},
	}})
	blockedRole := "admin"
	calls := 0
	rule, err := NewRule("temporary_role", RuleSpec{Check: func(_ context.Context, input RuleInput) (*Violation, error) {
		calls++
		group, ok := input.LookupField("group")
		if !ok {
			t.Fatal("missing related field lookup")
		}
		if input.Value == blockedRole && group.Value == "guest" {
			return &Violation{Params: map[string]string{"group": "访客组"}}, nil
		}
		return nil, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	opts := StructOptions{Locale: "zh-CN", ExtraRules: map[string][]Rule{"role": {rule}}}
	fields, err := v.ValidateStruct(ctx, form{Group: " GUEST "}, opts)
	if err != nil || len(fields) != 1 || fields[0].Rule != "required" || calls != 0 {
		t.Fatalf("tag bail got %v, %v; callback called %d times", fields, err, calls)
	}
	input := form{Role: " ADMIN ", Group: " GUEST "}
	fields, err = v.ValidateStruct(ctx, input, opts)
	if err != nil || len(fields) != 1 || fields[0].Rule != "temporary_role" ||
		fields[0].Message != "访客组 不允许使用此 角色。" || calls != 1 {
		t.Fatalf("temporary check got %v, %v; callback called %d times", fields, err, calls)
	}
	fields, err = v.ValidateStruct(ctx, input)
	if err != nil || len(fields) != 0 || calls != 1 {
		t.Fatalf("next call got %v, %v; callback called %d times", fields, err, calls)
	}
	if _, err := v.CompileRule(rule.Name()); err == nil {
		t.Fatal("temporary check was unexpectedly registered")
	}
}

func TestNewRuleErrorsAndImplicitChecks(t *testing.T) {
	cannotCheck := errors.New("comparison unavailable")
	check := func(context.Context, RuleInput) (*Violation, error) { return nil, cannotCheck }
	for _, tc := range []struct {
		name  string
		check RuleCheckFunc
	}{
		{"", check}, {"has space", check}, {"dive", check}, {"nullable", check}, {"email", check}, {"optional_check", nil},
	} {
		if _, err := NewRule(tc.name, RuleSpec{Check: tc.check}); err == nil {
			t.Errorf("invalid rule %q was accepted", tc.name)
		}
	}

	rule, err := NewRule("implicit_check", RuleSpec{Check: check, Implicit: true})
	if err != nil {
		t.Fatal(err)
	}
	v := newTestValidator(t, Config{})
	input := struct {
		Value *string `validate:"nullable"`
	}{}
	fields, err := v.ValidateStruct(context.Background(), input, StructOptions{ExtraRules: map[string][]Rule{"Value": {rule}}})
	if len(fields) != 0 || !errors.Is(err, cannotCheck) {
		t.Fatalf("implicit check got %v, %v", fields, err)
	}
}

func TestExtraRulesUsePreparedValuesAndStayRequestScoped(t *testing.T) {
	type form struct {
		Role    string `json:"role" label:"角色" prepare:"trim|lowercase" validate:"bail|required|in:member,admin"`
		Confirm string `json:"confirm" prepare:"trim|lowercase"`
	}
	v := newTestValidator(t, Config{})
	ctx := context.Background()
	input := form{Role: " ADMIN ", Confirm: " Admin "}
	opts := StructOptions{Locale: "zh-CN", ExtraRules: map[string][]Rule{
		"role":    compileTestRules(t, v, "not_in:admin"),
		"confirm": compileTestRules(t, v, "same:role"),
	}}
	fields, err := v.ValidateStruct(ctx, &input, opts)
	if err != nil || len(fields) != 1 || fields[0].Path != "role" ||
		fields[0].Rule != "not_in" || fields[0].Message != "角色 所选的值不可用。" {
		t.Fatalf("got %v, %v", fields, err)
	}
	fields, err = v.ValidateStruct(ctx, &input)
	if err != nil || len(fields) != 0 || input.Role != " ADMIN " || input.Confirm != " Admin " {
		t.Fatalf("next call got %v, %v; input=%+v", fields, err, input)
	}

	opts.PrepareForm = func(_ context.Context, value any) (any, error) {
		input := value.(form)
		input.Role, input.Confirm = " MEMBER ", " member "
		return input, nil
	}
	fields, err = v.ValidateStruct(ctx, input, opts)
	if err != nil || len(fields) != 0 {
		t.Fatalf("with whole-form preprocessing got %v, %v", fields, err)
	}
}

func TestExtraRulesShareBailAndFieldErrors(t *testing.T) {
	var queried []any
	v := newTestValidator(t, Config{DatabaseLookup: DatabaseLookupFunc(func(_ context.Context, query DatabaseQuery) (bool, error) {
		queried = append(queried, query.Value)
		return true, nil
	})})
	type form struct {
		Email string `json:"email" prepare:"trim|lowercase" validate:"bail|required|email"`
		Code  string `json:"code" validate:"min:4"`
	}
	opts := StructOptions{ExtraRules: map[string][]Rule{
		"email": compileTestRules(t, v, "unique:users,email"),
		"code":  compileTestRules(t, v, "regex:'^[0-9]+$'"),
	}}
	ctx := context.Background()
	fields, err := v.ValidateStruct(ctx, form{" INVALID ", "ab"}, opts)
	if err != nil || len(fields) != 3 || fields[0].Rule != "email" ||
		fields[1].Rule != "min" || fields[2].Rule != "regex" || len(queried) != 0 {
		t.Fatalf("got %v, %v; queries=%v", fields, err, queried)
	}
	antDesignFields := fields.AntDesignFields()
	if len(antDesignFields) != 2 || len(antDesignFields[1].Errors) != 2 {
		t.Fatalf("Ant Design fields: %+v", antDesignFields)
	}
	fields, err = v.ValidateStruct(ctx, form{" EXAMPLE@EXAMPLE.ORG ", "1234"}, opts)
	if err != nil || len(fields) != 1 || fields[0].Rule != "unique" ||
		!slices.Equal(queried, []any{"example@example.org"}) {
		t.Fatalf("got %v, %v; queries=%v", fields, err, queried)
	}
}

func TestExtraRulesMatchNestedAndPrimitiveListPaths(t *testing.T) {
	type user struct {
		Email   string `json:"email" prepare:"trim|lowercase" validate:"required|email"`
		Confirm string `json:"confirm" prepare:"trim|lowercase"`
	}
	type form struct {
		Users []*user  `json:"users"`
		Tags  []string `json:"tags" prepare:"dive|trim|uppercase"`
	}
	opts := StructOptions{ExtraRules: map[string][]Rule{}}
	v := newTestValidator(t, Config{})
	opts.ExtraRules["users.*.confirm"] = compileTestRules(t, v, "same:email")
	opts.ExtraRules["tags"] = compileTestRules(t, v, "dive|min:2")
	opts.ExtraRules["tags.*"] = compileTestRules(t, v, "in:GO,RUST")
	opts.ExtraRules["tags.0"] = compileTestRules(t, v, "regex:'^G'")
	opts.ExtraRules["tags.12"] = compileTestRules(t, v, "required")
	input := form{
		Users: []*user{{" DEV@EXAMPLE.ORG ", " dev@example.org "}, nil, {" OPS@EXAMPLE.ORG ", " d@example.org "}},
		Tags:  []string{" r ", " go "},
	}
	fields, err := v.ValidateStruct(context.Background(), &input, opts)
	if err != nil {
		t.Fatal(err)
	}
	var actual []string
	for _, field := range fields {
		actual = append(actual, field.Path+":"+field.Rule)
	}
	want := []string{"users.2.confirm:same", "tags.0:min", "tags.0:in", "tags.0:regex"}
	if !slices.Equal(actual, want) {
		t.Fatalf("got %v; want %v", actual, want)
	}
}

func TestExtraRulesRejectInvalidPathsAndChains(t *testing.T) {
	type user struct {
		Email string `json:"email"`
	}
	type form struct {
		Users []user `json:"users"`
		Email string `json:"email"`
		Skip  string `validate:"-"`
	}
	ctx := context.Background()
	v := newTestValidator(t, Config{})
	for _, tc := range []struct{ path, rules, want string }{
		{"users.*.typo", "required", "unknown field path"},
		{"*.email", "required", "unknown field path"},
		{"Skip", "required", "unknown field path"},
		{"users.*.email", "same:typo", "unknown related field"},
		{"email", "dive|required", "requires an array or slice"},
		{"email", "", "uncompiled rule"},
	} {
		rules := []Rule{{}}
		if tc.rules != "" {
			rules = compileTestRules(t, v, tc.rules)
		}
		fields, err := v.ValidateStruct(ctx, form{}, StructOptions{ExtraRules: map[string][]Rule{tc.path: rules}})
		if err == nil || !strings.Contains(err.Error(), tc.want) || len(fields) != 0 {
			t.Errorf("%s: got %v, %v; expected %q", tc.path, fields, err, tc.want)
		}
	}
}
