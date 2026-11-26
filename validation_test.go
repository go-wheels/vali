package vali_test

import (
	"context"
	"errors"
	"testing"

	"github.com/go-wheels/vali"
)

func TestValidateFieldWithRulesOptions(t *testing.T) {
	ctx := context.Background()
	v, err := vali.New(vali.Config{
		DatabaseLookup: vali.DatabaseLookupFunc(func(_ context.Context, q vali.DatabaseQuery) (bool, error) {
			if q.Table != "users" || q.Column != "email" || q.Value != "a@example.org" ||
				q.Exclusion == nil || q.Exclusion.Column != "id" || q.Exclusion.Value != 1 {
				t.Fatalf("unexpected database query: %+v", q)
			}
			return true, nil
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	rules, err := v.CompileRules("dive|required|same:email|unique:users,email")
	if err != nil {
		t.Fatal(err)
	}
	fields, err := v.ValidateFieldWithRules(ctx, []string{"original"}, rules, vali.FieldOptions{
		Path: "users",
		PrepareValue: func(context.Context, any) (any, error) {
			return []string{"a@example.org"}, nil
		},
		LookupField: func(path string) (vali.RelatedField, bool) {
			return vali.RelatedField{Path: path, Value: "a@example.org"}, path == "email"
		},
		UniqueExclusions: map[string]vali.RowExclusion{"users.0": {Column: "id", Value: 1}},
	})
	if err != nil || len(fields) != 1 || fields[0].Path != "users.0" || fields[0].Rule != "unique" {
		t.Fatalf("fields = %+v, err = %v", fields, err)
	}
	if antDesignFields := fields.AntDesignFields(); len(antDesignFields) != 1 || len(antDesignFields[0].Name) != 2 || antDesignFields[0].Name[1] != uint64(0) {
		t.Fatalf("Ant Design payload = %+v", antDesignFields)
	}
}

func TestValidateFieldReportsExecutionErrorsSeparately(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	fields, err := vali.ValidateField(ctx, "", "required")
	if fields != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("fields = %+v, err = %v", fields, err)
	}
	fields, err = vali.ValidateField(context.Background(), "", "unknown_rule")
	if fields != nil || err == nil {
		t.Fatalf("fields = %+v, err = %v", fields, err)
	}
	fields, err = vali.ValidateField(context.Background(), "", "required", vali.FieldOptions{Path: "users.*"})
	if fields != nil || err == nil {
		t.Fatalf("fields = %+v, err = %v", fields, err)
	}
}
