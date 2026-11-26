package vali

import (
	"context"
	"fmt"
	"math"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

func newTestValidator(t *testing.T, config Config) *Validator {
	t.Helper()
	v, err := New(config)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func compileTestRules(t *testing.T, v *Validator, raw string) []Rule {
	t.Helper()
	rules, err := v.CompileRules(raw)
	if err != nil {
		t.Fatal(err)
	}
	return rules
}

func TestBuiltinsAgreeBetweenEntrypoints(t *testing.T) {
	v := newTestValidator(t, Config{})
	zero := 0
	cases := []struct {
		tag      string
		value    any
		other    any
		failures []string
	}{
		{tag: "required", value: false},
		{tag: "required", value: &zero},
		{tag: "required", value: []int{}, failures: []string{"required"}},
		{tag: "required", value: " \t", failures: []string{"required"}},
		{tag: "nullable|required|string", value: (*int)(nil), failures: []string{"required"}},
		{tag: "nullable|email", value: (*string)(nil)},
		{tag: "email", value: (*string)(nil), failures: []string{"email"}},
		{tag: "string", value: "text"},
		{tag: "string", value: 12, failures: []string{"string"}},
		{tag: "boolean", value: "0"},
		{tag: "boolean", value: "yes", failures: []string{"boolean"}},
		{tag: "array", value: [1]string{"a"}},
		{tag: "array", value: map[string]string{"a": "b"}, failures: []string{"array"}},
		{tag: "integer", value: "+001"},
		{tag: "integer", value: 5.0},
		{tag: "integer", value: "1.5", failures: []string{"integer"}},
		{tag: "numeric", value: "-.12e-99"},
		{tag: "numeric", value: math.Inf(1), failures: []string{"numeric"}},
		{tag: "numeric", value: math.NaN(), failures: []string{"numeric"}},
		{tag: "numeric", value: true, failures: []string{"numeric"}},
		{tag: "email", value: "one@example.org"},
		{tag: "email", value: "Name <one@example.org>", failures: []string{"email"}},
		{tag: "url", value: "https://example.org/q?a=b#c"},
		{tag: "url", value: "//example.org", failures: []string{"url"}},
		{tag: "min:2", value: "汉🙂"},
		{tag: "min:2", value: "汉", failures: []string{"min"}},
		{tag: "max:2", value: []int{1, 2}},
		{tag: "max:2", value: []int{1, 2, 3}, failures: []string{"max"}},
		{tag: "between:-0.35,-0.3", value: -0.35},
		{tag: "between:-0.35,-0.3", value: -0.355, failures: []string{"between"}},
		{tag: "size:2", value: "二字"},
		{tag: "min:10|integer", value: "2", failures: []string{"min"}},
		{tag: "max:18446744073709551614", value: uint64(math.MaxUint64), failures: []string{"max"}},
		{tag: "numeric|size:1e9999999999999999999", value: "10e9999999999999999998"},
		{tag: "numeric|between:0,1e-9999999999999999999", value: "2e-9999999999999999999", failures: []string{"between"}},
		{tag: "in:0,1", value: 1},
		{tag: "in:red,blue", value: "green", failures: []string{"in"}},
		{tag: "not_in:admin,root", value: "admin", failures: []string{"not_in"}},
		{tag: "not_in:admin,root", value: "member"},
		{tag: "regex:'^(CN|US)-[0-9]{2,4}$'", value: "CN-123"},
		{tag: "regex:'^(CN|US)-[0-9]{2,4}$'", value: "CN-12345", failures: []string{"regex"}},
		{tag: "same:other", value: "same", other: "same"},
		{tag: "same:other", value: "same", failures: []string{"same"}},
		{tag: "different:other", value: 5, other: 6},
		{tag: "different:other", value: 5, other: 5, failures: []string{"different"}},
		{tag: "confirmed:other", value: "same", other: "same"},
		{tag: "confirmed:other", value: "same", other: "else", failures: []string{"confirmed"}},
		{tag: "required_if:other,admin,owner", value: "", other: "admin", failures: []string{"required_if"}},
		{tag: "required_if:other,admin,owner", value: "", other: "staff"},
		{tag: "nullable|required_unless:other,admin", other: "staff", failures: []string{"required_unless"}},
		{tag: "required_unless:other,admin", other: "admin"},
		{tag: "required_with:other", other: false, failures: []string{"required_with"}},
		{tag: "required_with:other"},
		{tag: "required_without:other", failures: []string{"required_without"}},
		{tag: "required_without:other", other: 0},
		{tag: "bail|email|min:20", value: "bad", failures: []string{"email"}},
		{tag: "email|min:20", value: "bad", failures: []string{"email", "min"}},
		{tag: "in:''", value: " "},
	}
	for _, tc := range cases {
		t.Run(fmt.Sprintf("%s/%T/%v", tc.tag, tc.value, tc.value), func(t *testing.T) {
			rules := compileTestRules(t, v, tc.tag)
			direct, err := v.ValidateFieldWithRules(context.Background(), tc.value, rules, FieldOptions{
				LookupField: func(path string) (RelatedField, bool) {
					return RelatedField{Path: path, Value: tc.other}, path == "other"
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			var names []string
			for _, failure := range direct {
				names = append(names, failure.Rule)
			}
			if !slices.Equal(names, tc.failures) {
				t.Fatalf("got %v, want %v", names, tc.failures)
			}
			anyType := reflect.TypeOf((*any)(nil)).Elem()
			typ := reflect.StructOf([]reflect.StructField{
				{Name: "Value", Type: anyType, Tag: reflect.StructTag(`json:"value" validate:` + strconv.Quote(tc.tag))},
				{Name: "Other", Type: anyType, Tag: `json:"other"`},
			})
			form := reflect.New(typ).Elem()
			form.Field(0).Set(reflect.ValueOf(&tc.value).Elem())
			form.Field(1).Set(reflect.ValueOf(&tc.other).Elem())
			fields, err := v.ValidateStruct(context.Background(), form.Interface())
			if err != nil || !reflect.DeepEqual(direct, fields) {
				t.Fatalf("struct returned %v, %v; direct returned %v", fields, err, direct)
			}
		})
	}
}

func TestTagsEndToEnd(t *testing.T) {
	v := newTestValidator(t, Config{})
	type form struct {
		Name  string `validate:"in:'O''Reilly','a|b,c',' a:b ',''"`
		Code  string `validate:"regex:'^\\d{2,4}$'"`
		Quote string `validate:"in:'a\"b'"`
		Path  string `validate:"in:'C:\\tmp'"`
		Grave string `validate:"in:'\x60'"`
	}
	for _, name := range []string{"O'Reilly", "a|b,c", " a:b "} {
		result, err := v.ValidateStruct(context.Background(), form{name, "042", "a\"b", "C:\\tmp", "\x60"})
		if err != nil || len(result) != 0 {
			t.Fatalf("%q: got %v, %v", name, result, err)
		}
	}
	tags, err := parseExpression("in:'', '  ', bare : value ")
	if want := []string{"", "  ", "bare : value"}; err != nil || len(tags) != 1 || !slices.Equal(tags[0].args, want) {
		t.Fatalf("got %#v, %v; want %#v", tags, err, want)
	}
	for _, tag := range []string{
		`validate:"in:"`, `validate:"in:a,,b"`, `validate:"in:a,"`,
		`validate:"in:'a"`, `validate:"in:'a'b"`, `validate:"in:O'Reilly"`,
		`validate:"required|"`, `validate:"required||email"`, `validate:""`,
		`validate:"unknown"`, `validate:"between:5,2"`, `validate:"email:arg"`,
		`validate:"regex:'[a'"`, `validate:"regex:'^\d+$'"`, `validate:"in:x"validate:"in:y"`,
	} {
		typ := reflect.StructOf([]reflect.StructField{{Name: "A", Type: reflect.TypeOf(""), Tag: reflect.StructTag(tag)}})
		got, err := v.ValidateStruct(context.Background(), reflect.New(typ).Elem().Interface())
		if err == nil || len(got) != 0 {
			t.Errorf("%s got %v, %v; expected configuration error", tag, got, err)
		}
	}
}

func TestNestedTraversalAndRelatedPaths(t *testing.T) {
	v := newTestValidator(t, Config{})
	type row struct {
		Email   string `json:"email" validate:"required|email"`
		Confirm string `json:"confirm" validate:"confirmed:email"`
		Role    string `json:"role" validate:"same:/role"`
	}
	type form struct {
		Role   string  `json:"role"`
		Users  []*row  `json:"users" validate:"array|dive|bail|required"`
		Ranks  [][]int `json:"ranks" validate:"dive|dive|min:2"`
		First  string  `json:"first" validate:"same:users.0.email"`
		Absent string  `json:"absent" validate:"same:users.8.email"`
		Home   *row    `json:"home"`
		Hidden *row    `json:"-"`
		Skip   *row    `validate:"-"`
	}
	shared := &row{Email: "bad", Confirm: "other", Role: "staff"}
	got, err := v.ValidateStruct(context.Background(), &form{
		Role: "admin", Users: []*row{shared, nil, shared}, Ranks: [][]int{{1, 2}},
		First: "bad", Absent: "different", Hidden: shared, Skip: shared,
	})
	if err != nil {
		t.Fatal(err)
	}
	var actual []string
	for _, item := range got {
		actual = append(actual, item.Path+":"+item.Rule)
	}
	want := []string{"users.0.email:email", "users.0.confirm:confirmed", "users.0.role:same", "users.1:required",
		"users.2.email:email", "users.2.confirm:confirmed", "users.2.role:same", "ranks.0.0:min", "absent:same"}
	if !slices.Equal(actual, want) {
		t.Errorf("got %v, want %v", actual, want)
	}

	type node struct {
		Name     string `validate:"required"`
		Next     *node
		Children []node
	}
	cycle := &node{}
	cycle.Next = cycle
	cycle.Children = make([]node, 1)
	cycle.Children[0].Children = cycle.Children
	got, err = v.ValidateStruct(context.Background(), cycle)
	if err != nil || len(got) != 2 || got[0].Path != "Name" || got[1].Path != "Children.0.Name" {
		t.Fatalf("cycle got %v, %v", got, err)
	}
}

func TestStaticSchemaErrors(t *testing.T) {
	v := newTestValidator(t, Config{})
	type badChild struct {
		Name string `validate:"same:missing"`
	}
	for _, value := range []any{
		struct{ Nested *badChild }{},
		struct{ Nested []badChild }{},
		struct {
			Count int `validate:"dive|min:1"`
		}{},
		struct {
			Items []int `validate:"dive"`
		}{},
		map[string]string{}, (*struct{})(nil), nil,
	} {
		if got, err := v.ValidateStruct(context.Background(), value); err == nil || len(got) != 0 {
			t.Errorf("%T unexpectedly returned %v, %v", value, got, err)
		}
	}
	typ := reflect.StructOf([]reflect.StructField{
		{Name: "First", Type: reflect.TypeOf(""), Tag: `json:"same"`},
		{Name: "Second", Type: reflect.TypeOf(""), Tag: `json:"same,omitempty"`},
	})
	if _, err := v.ValidateStruct(context.Background(), reflect.New(typ).Elem().Interface()); err == nil {
		t.Fatal("duplicate field path did not fail")
	}
}

func TestLocalizationAndConcurrentSchemaCompilation(t *testing.T) {
	var compiles atomic.Int32
	catalogs := map[string]Catalog{
		"en": {Labels: map[string]string{"form.email": "Email", "form.other": "Confirmation"}},
		"zh_CN": {
			Labels:   map[string]string{"form.email": "邮箱", "form.other": "确认邮箱"},
			Messages: map[string]string{"prefix_message": ":attribute 应以 :prefix 开头。"},
			FieldMessages: map[string]map[string]string{
				"users.*.email": {"email": "请检查 :attribute 的格式。"},
				"users.0.email": {"email": "首页的 :attribute 格式不正确。"},
			},
		},
	}
	v := newTestValidator(t, Config{Catalogs: catalogs, FallbackLocale: "en", RuleFactories: map[string]RuleFactory{
		"prefix": func(args []string) (RuleSpec, error) {
			compiles.Add(1)
			if len(args) != 1 {
				return RuleSpec{}, fmt.Errorf("expected one argument")
			}
			return RuleSpec{Check: func(_ context.Context, in RuleInput) (*Violation, error) {
				if strings.HasPrefix(in.Value.(string), args[0]) {
					return nil, nil
				}
				return &Violation{MessageKey: "prefix_message", Params: map[string]string{"prefix": args[0]}}, nil
			}}, nil
		},
	}})
	catalogs["en"].Labels["form.email"] = "changed"
	catalogs["zh_CN"].FieldMessages["users.0.email"]["email"] = "changed"
	type row struct {
		Email string `json:"email" label:"form.email" validate:"email|same:other|prefix:'A:attribute'"`
		Other string `json:"other" label:"form.other"`
	}
	form := struct {
		Users []row `json:"users"`
	}{Users: []row{{"bad", "other"}, {"bad", "other"}}}
	var wg sync.WaitGroup
	for i := 0; i < 24; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			locale := "en-US"
			if i%2 == 0 {
				locale = "zh-Hans"
			}
			got, err := v.ValidateStruct(context.Background(), form, StructOptions{Locale: locale})
			if err != nil || len(got) != 6 {
				t.Errorf("%s got %v, %v", locale, got, err)
				return
			}
			if locale == "zh-Hans" {
				if got[0].Message != "首页的 邮箱 格式不正确。" || got[1].Message != "邮箱 必须与 确认邮箱 一致。" ||
					got[2].Message != "邮箱 应以 A:attribute 开头。" || got[3].Message != "请检查 邮箱 的格式。" {
					t.Errorf("unexpected zh errors: %#v", got)
				}
			} else if got[0].Message != "Email must be a valid email address." || got[2].Message != "Email is invalid." {
				t.Errorf("unexpected en errors: %#v", got)
			}
		}()
	}
	wg.Wait()
	if got := compiles.Load(); got != 1 {
		t.Fatalf("compiled schema rule %d times; want 1", got)
	}
	field, err := v.ValidateFieldWithRules(context.Background(), "", compileTestRules(t, v, "required"),
		FieldOptions{Label: "form.email", Locale: "missing"})
	if err != nil || len(field) != 1 || field[0].Message != "Email is required." {
		t.Fatalf("fallback got %v, %v", field, err)
	}
}

func TestMixedWildcardOverridesInBothEntrypoints(t *testing.T) {
	v := newTestValidator(t, Config{Catalogs: map[string]Catalog{
		"en": {FieldMessages: map[string]map[string]string{
			"groups.*.users.*.name": {"required": "All groups"},
			"groups.*.users.1.name": {"required": "Second member"},
			"groups.0.users.*.name": {"required": "First group"},
			"groups.0.users.0.name": {"required": "First member"},
		}},
	}})
	type member struct {
		Name string `json:"name" validate:"required"`
	}
	type group struct {
		Users []member `json:"users"`
	}
	form := struct {
		Groups []group `json:"groups"`
	}{Groups: []group{{Users: make([]member, 2)}, {Users: make([]member, 2)}}}
	fields, err := v.ValidateStruct(context.Background(), form)
	if err != nil {
		t.Fatal(err)
	}
	rules := compileTestRules(t, v, "required")
	var messages []string
	for _, field := range fields {
		messages = append(messages, field.Message)
		direct, err := v.ValidateFieldWithRules(context.Background(), nil, rules, FieldOptions{Path: field.Path})
		if err != nil || len(direct) != 1 || !reflect.DeepEqual(direct[0], field) {
			t.Fatalf("%s: direct got %v, %v; want %v", field.Path, direct, err, field)
		}
	}
	if want := []string{"First member", "Second member", "All groups", "Second member"}; !slices.Equal(messages, want) {
		t.Errorf("got %v, want %v", messages, want)
	}
}
