package vali

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"reflect"
	"strings"
)

// SQLRowQuerier is implemented by *sql.DB, *sql.Tx and *sql.Conn.
type SQLRowQuerier interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

// SQLDialect selects the SQL identifier and placeholder syntax.
type SQLDialect string

const (
	PostgreSQL SQLDialect = "postgres"
	MySQL      SQLDialect = "mysql"
	SQLite     SQLDialect = "sqlite"
)

// SQLLookup implements DatabaseLookup with database/sql.
type SQLLookup struct {
	db      SQLRowQuerier
	dialect SQLDialect
}

// NewSQLLookup constructs an adapter for an existing connection or transaction.
func NewSQLLookup(db SQLRowQuerier, dialect SQLDialect) (*SQLLookup, error) {
	if isNilValue(reflect.ValueOf(db)) {
		return nil, fmt.Errorf("SQL lookup requires a database or transaction")
	}
	switch dialect {
	case PostgreSQL, MySQL, SQLite:
		return &SQLLookup{db: db, dialect: dialect}, nil
	default:
		return nil, fmt.Errorf("unsupported SQL dialect %q", dialect)
	}
}

// Lookup executes a parameterized presence query.
func (s *SQLLookup) Lookup(ctx context.Context, request DatabaseQuery) (bool, error) {
	if s == nil || s.db == nil {
		return false, fmt.Errorf("SQL lookup requires a database or transaction")
	}
	if ctx == nil {
		return false, fmt.Errorf("SQL lookup requires a non-nil context")
	}
	if err := validateLookupTarget(request.Table, request.Column); err != nil {
		return false, err
	}
	if err := validateRowExclusion(request.Exclusion); err != nil {
		return false, err
	}
	table := strings.Split(request.Table, ".")
	for i, part := range table {
		table[i] = s.quoteIdentifier(part)
	}
	query := "SELECT 1 FROM " + strings.Join(table, ".") +
		" WHERE " + s.quoteIdentifier(request.Column) + " = " + s.placeholder(1)
	args := []any{request.Value}
	if request.Exclusion != nil {
		column := s.quoteIdentifier(request.Exclusion.Column)
		query += " AND (" + column + " IS NULL OR " + column + " <> " + s.placeholder(2) + ")"
		args = append(args, request.Exclusion.Value)
	}

	var one int
	err := s.db.QueryRowContext(ctx, query+" LIMIT 1", args...).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

func (s *SQLLookup) quoteIdentifier(identifier string) string {
	if s.dialect == MySQL {
		return "\x60" + identifier + "\x60"
	}
	return "\"" + identifier + "\""
}

func (s *SQLLookup) placeholder(index int) string {
	if s.dialect == PostgreSQL {
		return fmt.Sprintf("$%d", index)
	}
	return "?"
}
