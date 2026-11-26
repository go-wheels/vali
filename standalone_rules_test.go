package vali_test

import (
	"context"
	"errors"
	"testing"

	"github.com/go-wheels/vali"
)

func TestStandaloneValidators(t *testing.T) {
	for _, tt := range []struct {
		name           string
		check          func(any) (bool, error)
		valid, invalid any
	}{
		{"required", vali.Required, false, " "},
		{"string", vali.String, "", 42},
		{"boolean", vali.Boolean, "1", "true"},
		{"array", vali.Array, []int{}, []int(nil)},
		{"numeric", vali.Numeric, "1.2", false},
		{"integer", vali.Integer, float64(42), "1.2"},
		{"email", vali.Email, "user@example.com", " "},
		{"url", vali.URL, "https://example.com", "//example.com"},
		{"min", func(v any) (bool, error) { return vali.Min(v, "2") }, "你好", "你"},
		{"max", func(v any) (bool, error) { return vali.Max(v, "2") }, 2, 3},
		{"between", func(v any) (bool, error) { return vali.Between(v, "2", "4") }, 4, 5},
		{"size", func(v any) (bool, error) { return vali.Size(v, "2") }, []int{1, 2}, []int{1}},
		{"in", func(v any) (bool, error) { return vali.In(v, "1", "2") }, 1, 3},
		{"not_in", func(v any) (bool, error) { return vali.NotIn(v, "1") }, 2, 1},
		{"regex", func(v any) (bool, error) { return vali.Regex(v, `^\d+$`) }, "42", "forty-two"},
		{"same", func(v any) (bool, error) { return vali.Same(v, 42) }, 42, "42"},
		{"different", func(v any) (bool, error) { return vali.Different(v, 42) }, "42", 42},
		{"confirmed", func(v any) (bool, error) { return vali.Confirmed(v, "secret") }, "secret", nil},
		{"required_if", func(v any) (bool, error) { return vali.RequiredIf(v, "admin", "owner", "admin") }, 0, nil},
		{"required_unless", func(v any) (bool, error) { return vali.RequiredUnless(v, "admin", "owner") }, 0, nil},
		{"required_with", func(v any) (bool, error) { return vali.RequiredWith(v, nil, 0) }, 0, nil},
		{"required_without", func(v any) (bool, error) { return vali.RequiredWithout(v, 0, nil) }, 0, nil},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ok, err := tt.check(tt.valid)
			if !ok || err != nil {
				t.Errorf("valid: got (%v, %v), want (true, nil)", ok, err)
			}
			ok, err = tt.check(tt.invalid)
			if ok || !errors.Is(err, vali.ErrInvalidValue) {
				t.Errorf("invalid: got (%v, %v), want (false, ErrInvalidValue)", ok, err)
			}
		})
	}
}

func TestStandaloneInvalidArguments(t *testing.T) {
	for _, check := range []func() (bool, error){
		func() (bool, error) { return vali.Regex("hello", "[") },
		func() (bool, error) { return vali.Between(2, "4", "2") },
		func() (bool, error) { return vali.In("hello") },
	} {
		ok, err := check()
		if ok || err == nil || errors.Is(err, vali.ErrInvalidValue) {
			t.Errorf("got (%v, %v), want an argument error", ok, err)
		}
	}
}

func TestStandaloneDatabaseValidators(t *testing.T) {
	ctx := context.Background()
	id := 42
	exclusion := &vali.RowExclusion{Column: "id", Value: id}
	lookup := vali.DatabaseLookupFunc(func(_ context.Context, query vali.DatabaseQuery) (bool, error) {
		if query.Table != "users" || query.Column != "id" || query.Value != id {
			t.Errorf("unexpected query: %+v", query)
		}
		if query.Exclusion != nil && *query.Exclusion != *exclusion {
			t.Errorf("unexpected exclusion: %+v", query.Exclusion)
		}
		return query.Exclusion == nil, nil
	})

	if ok, err := vali.Exists(ctx, lookup, &id, "users", "id"); !ok || err != nil {
		t.Errorf("exists: got (%v, %v), want (true, nil)", ok, err)
	}
	if ok, err := vali.Unique(ctx, lookup, &id, "users", "id", exclusion); !ok || err != nil {
		t.Errorf("unique with exclusion: got (%v, %v), want (true, nil)", ok, err)
	}
	if ok, err := vali.Unique(ctx, lookup, id, "users", "id", nil); ok || !errors.Is(err, vali.ErrInvalidValue) {
		t.Errorf("unique: got (%v, %v), want (false, ErrInvalidValue)", ok, err)
	}

	lookupErr := errors.New("lookup failed")
	failingChecker := vali.DatabaseLookupFunc(func(context.Context, vali.DatabaseQuery) (bool, error) {
		return false, lookupErr
	})
	if ok, err := vali.Exists(ctx, failingChecker, id, "users", "id"); ok || !errors.Is(err, lookupErr) {
		t.Errorf("lookup failure: got (%v, %v), want (false, lookup error)", ok, err)
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if ok, err := vali.Exists(canceled, lookup, id, "users", "id"); ok || !errors.Is(err, context.Canceled) {
		t.Errorf("canceled lookup: got (%v, %v), want (false, context.Canceled)", ok, err)
	}
}
