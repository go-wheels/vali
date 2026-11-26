package vali

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestDatabaseRulesAtBothEntrypoints(t *testing.T) {
	var seen []DatabaseQuery
	queryErr := errors.New("database unavailable")
	v := newTestValidator(t, Config{DatabaseLookup: DatabaseLookupFunc(func(_ context.Context, query DatabaseQuery) (bool, error) {
		seen = append(seen, query)
		switch query.Value {
		case "error@example.org":
			return false, queryErr
		case "taken@example.org":
			return query.Exclusion == nil, nil
		case 2:
			return true, nil
		default:
			return false, nil
		}
	})})
	type form struct {
		Email string `json:"email" validate:"bail|required|email|unique:users,email"`
		Role  int    `json:"role" validate:"exists:roles,id"`
	}
	ctx := context.Background()
	unique := compileTestRules(t, v, "bail|required|email|unique:users,email")
	exists := compileTestRules(t, v, "exists:roles,id")
	cases := []struct {
		form  form
		opts  StructOptions
		rules []string
		calls int
	}{
		{form: form{"taken@example.org", 2}, rules: []string{"unique"}, calls: 2},
		{form: form{"free@example.org", 3}, rules: []string{"exists"}, calls: 2},
		{form: form{"not-email", 2}, rules: []string{"email"}, calls: 1},
		{form: form{"taken@example.org", 2}, opts: StructOptions{UniqueExclusions: map[string]RowExclusion{"email": {"id", 17}}}, calls: 2},
	}
	for _, tc := range cases {
		seen = nil
		got, err := v.ValidateStruct(ctx, tc.form, tc.opts)
		if err != nil || len(seen) != tc.calls {
			t.Fatalf("%v: got %v, %v with calls %v", tc.form, got, err, seen)
		}
		var names []string
		for _, item := range got {
			names = append(names, item.Rule)
		}
		if !slices.Equal(names, tc.rules) {
			t.Errorf("got %v; want %v", names, tc.rules)
		}
		structCalls := slices.Clone(seen)
		seen = nil
		email, err := v.ValidateFieldWithRules(ctx, tc.form.Email, unique, FieldOptions{Path: "email", UniqueExclusions: tc.opts.UniqueExclusions})
		if err != nil {
			t.Fatal(err)
		}
		role, err := v.ValidateFieldWithRules(ctx, tc.form.Role, exists, FieldOptions{Path: "role"})
		if err != nil || !slices.EqualFunc(append(email, role...), got, func(a, b FieldError) bool {
			return reflect.DeepEqual(a, b)
		}) || !reflect.DeepEqual(seen, structCalls) {
			t.Fatalf("direct %v, %v, %v differs from struct %v, %v", email, role, err, got, structCalls)
		}
	}
	got, err := v.ValidateStruct(ctx, form{"error@example.org", 2})
	if !errors.Is(err, queryErr) || len(got) != 0 {
		t.Fatalf("lookup error got %v, %v", got, err)
	}
	seen = nil
	var nilID *int
	_, err = v.ValidateStruct(ctx, form{"taken@example.org", 2}, StructOptions{
		UniqueExclusions: map[string]RowExclusion{"email": {"id", nilID}},
	})
	if err == nil || len(seen) != 0 {
		t.Fatalf("invalid exclusion got err=%v, calls=%v", err, seen)
	}
	plain := newTestValidator(t, Config{})
	if _, err = plain.ValidateStruct(ctx, form{"good@example.org", 2}); err == nil {
		t.Fatal("missing lookup was not reported")
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err = v.ValidateStruct(cancelled, form{"good@example.org", 2}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled context got %v", err)
	}
}

type queryFunction func(context.Context, string, []driver.NamedValue) (driver.Rows, error)

type lookupConnector struct{ query queryFunction }

func (c lookupConnector) Connect(context.Context) (driver.Conn, error) {
	return lookupConn{query: c.query}, nil
}
func (lookupConnector) Driver() driver.Driver { return nil }

type lookupConn struct{ query queryFunction }

func (c lookupConn) QueryContext(ctx context.Context, text string, args []driver.NamedValue) (driver.Rows, error) {
	return c.query(ctx, text, args)
}
func (lookupConn) Prepare(string) (driver.Stmt, error) { return nil, driver.ErrSkip }
func (lookupConn) Close() error                        { return nil }
func (lookupConn) Begin() (driver.Tx, error)           { return lookupTx{}, nil }

type lookupTx struct{}

func (lookupTx) Commit() error   { return nil }
func (lookupTx) Rollback() error { return nil }

type lookupRows struct{ hasRow bool }

func (*lookupRows) Columns() []string { return []string{"present"} }
func (*lookupRows) Close() error      { return nil }
func (r *lookupRows) Next(values []driver.Value) error {
	if !r.hasRow {
		return io.EOF
	}
	r.hasRow = false
	values[0] = int64(1)
	return nil
}

func TestSQLLookupDialectsParameterBindingAndNullExclusion(t *testing.T) {
	fakeEmail := "name with ' and spaces@example.org"
	cases := []struct {
		dialect  SQLDialect
		expected string
	}{
		{PostgreSQL, `SELECT 1 FROM "public"."users" WHERE "email" = $1 AND ("id" IS NULL OR "id" <> $2) LIMIT 1`},
		{MySQL, "SELECT 1 FROM \x60public\x60.\x60users\x60 WHERE \x60email\x60 = ? AND (\x60id\x60 IS NULL OR \x60id\x60 <> ?) LIMIT 1"},
		{SQLite, `SELECT 1 FROM "public"."users" WHERE "email" = ? AND ("id" IS NULL OR "id" <> ?) LIMIT 1`},
	}
	for _, tc := range cases {
		t.Run(string(tc.dialect), func(t *testing.T) {
			calls := 0
			db := sql.OpenDB(lookupConnector{query: func(_ context.Context, text string, args []driver.NamedValue) (driver.Rows, error) {
				calls++
				expectedArgs := []driver.NamedValue{{Ordinal: 1, Value: fakeEmail}, {Ordinal: 2, Value: int64(17)}}
				if text != tc.expected || !reflect.DeepEqual(args, expectedArgs) {
					return nil, fmt.Errorf("got %s %v; want %s %v", text, args, tc.expected, expectedArgs)
				}
				return &lookupRows{hasRow: calls == 1}, nil
			}})
			defer db.Close()
			ctx := context.Background()
			query := DatabaseQuery{Table: "public.users", Column: "email", Value: fakeEmail, Exclusion: &RowExclusion{Column: "id", Value: 17}}
			lookup, err := NewSQLLookup(db, tc.dialect)
			if err != nil {
				t.Fatal(err)
			}
			if result, err := lookup.Lookup(ctx, query); err != nil || !result {
				t.Fatalf("DB lookup got %v, %v", result, err)
			}
			tx, err := db.BeginTx(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback()
			lookup, err = NewSQLLookup(tx, tc.dialect)
			if err != nil {
				t.Fatal(err)
			}
			if result, err := lookup.Lookup(ctx, query); err != nil || result {
				t.Fatalf("transaction lookup got %v, %v", result, err)
			}
			query.Column = "not valid"
			if _, err := lookup.Lookup(ctx, query); err == nil || calls != 2 {
				t.Fatalf("invalid identifier got err=%v, calls=%d", err, calls)
			}
		})
	}
}

func TestSQLLookupErrorsAndCancellation(t *testing.T) {
	driverErr := errors.New("test query failure")
	started := make(chan struct{})
	db := sql.OpenDB(lookupConnector{query: func(ctx context.Context, text string, _ []driver.NamedValue) (driver.Rows, error) {
		if !strings.HasSuffix(text, "LIMIT 1") {
			t.Errorf("unexpected SQL: %s", text)
		}
		if ctx.Value(testContextKey{}) != nil {
			close(started)
			<-ctx.Done()
			return nil, ctx.Err()
		}
		return nil, driverErr
	}})
	defer db.Close()
	lookup, err := NewSQLLookup(db, SQLite)
	if err != nil {
		t.Fatal(err)
	}
	query := DatabaseQuery{Table: "roles", Column: "id", Value: 1}
	if _, err := lookup.Lookup(context.Background(), query); !errors.Is(err, driverErr) {
		t.Fatalf("wanted driver error, got %v", err)
	}
	ctx, cancel := context.WithTimeout(context.WithValue(context.Background(), testContextKey{}, true), 5*time.Second)
	defer cancel()
	go func() {
		select {
		case <-started:
			cancel()
		case <-ctx.Done():
		}
	}()
	if _, err := lookup.Lookup(ctx, query); !errors.Is(err, context.Canceled) {
		t.Fatalf("wanted cancellation, got %v", err)
	}
	if _, err := NewSQLLookup((*sql.DB)(nil), SQLite); err == nil {
		t.Fatal("typed nil database was accepted")
	}
	if _, err := NewSQLLookup(db, SQLDialect("unknown")); err == nil {
		t.Fatal("unknown dialect was accepted")
	}
}

type testContextKey struct{}
