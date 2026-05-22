package sqlstore

import (
	"fmt"
	"net/url"
	"strings"
)

type Dialect interface {
	Name() string
	DriverName() string
	BindVar(index int) string
	TextType() string
	KeyTextType() string
	JSONType() string
	TimeType() string
	BoolType() string
	QuoteIdent(name string) string
	UpsertClause(conflictColumn string, updateColumns []string) string
	TableExistsSQL(tableName string) string
	CreateIndexSQL(indexName string, tableName string, columns []string) string
}

type Config struct {
	Backend    string
	DriverName string
	DSN        string
	Dialect    Dialect
}

type PostgresDialect struct{}
type MySQLDialect struct{}
type SQLiteDialect struct{}

func ConfigFromReference(reference string) (Config, error) {
	dialect, err := DialectFromReference(reference)
	if err != nil {
		return Config{}, err
	}
	return Config{
		Backend:    dialect.Name(),
		DriverName: dialect.DriverName(),
		DSN:        strings.TrimSpace(reference),
		Dialect:    dialect,
	}, nil
}

func DialectFromReference(reference string) (Dialect, error) {
	reference = strings.TrimSpace(reference)
	if reference == "" {
		return SQLiteDialect{}, nil
	}
	parsed, err := url.Parse(reference)
	if err != nil || parsed.Scheme == "" {
		if strings.Contains(reference, "://") {
			return nil, fmt.Errorf("invalid store reference %q", reference)
		}
		return SQLiteDialect{}, nil
	}
	switch strings.ToLower(parsed.Scheme) {
	case "postgres", "postgresql":
		return PostgresDialect{}, nil
	case "mysql":
		return MySQLDialect{}, nil
	case "sqlite", "file":
		return SQLiteDialect{}, nil
	default:
		return nil, fmt.Errorf("unsupported store backend %q", parsed.Scheme)
	}
}

func (PostgresDialect) Name() string       { return "postgres" }
func (PostgresDialect) DriverName() string { return "pgx" }
func (PostgresDialect) BindVar(index int) string {
	if index < 1 {
		index = 1
	}
	return fmt.Sprintf("$%d", index)
}
func (PostgresDialect) TextType() string    { return "text" }
func (PostgresDialect) KeyTextType() string { return "text" }
func (PostgresDialect) JSONType() string    { return "jsonb" }
func (PostgresDialect) TimeType() string    { return "timestamptz" }
func (PostgresDialect) BoolType() string    { return "boolean" }
func (PostgresDialect) QuoteIdent(name string) string {
	return quoteDouble(name)
}
func (PostgresDialect) UpsertClause(conflictColumn string, updateColumns []string) string {
	return standardUpsert(conflictColumn, updateColumns)
}
func (PostgresDialect) TableExistsSQL(tableName string) string {
	return fmt.Sprintf(`select case when exists (
  select 1 from information_schema.tables
  where table_schema = current_schema() and table_name = %s
) then 1 else 0 end as table_exists`, sqlLiteral(tableName))
}
func (d PostgresDialect) CreateIndexSQL(indexName string, tableName string, columns []string) string {
	return standardCreateIndexSQL(d, indexName, tableName, columns)
}

func (MySQLDialect) Name() string       { return "mysql" }
func (MySQLDialect) DriverName() string { return "mysql" }
func (MySQLDialect) BindVar(int) string { return "?" }
func (MySQLDialect) TextType() string   { return "mediumtext" }
func (MySQLDialect) KeyTextType() string {
	return "varchar(128)"
}
func (MySQLDialect) JSONType() string { return "json" }
func (MySQLDialect) TimeType() string { return "datetime(6)" }
func (MySQLDialect) BoolType() string { return "boolean" }
func (MySQLDialect) QuoteIdent(name string) string {
	return "`" + strings.ReplaceAll(name, "`", "``") + "`"
}
func (MySQLDialect) UpsertClause(_ string, updateColumns []string) string {
	assignments := make([]string, 0, len(updateColumns))
	for _, column := range updateColumns {
		column = strings.TrimSpace(column)
		if column == "" {
			continue
		}
		assignments = append(assignments, fmt.Sprintf("%s = values(%s)", column, column))
	}
	return "on duplicate key update " + strings.Join(assignments, ", ")
}
func (MySQLDialect) TableExistsSQL(tableName string) string {
	return fmt.Sprintf(`select case when exists (
  select 1 from information_schema.tables
  where table_schema = database() and table_name = %s
) then 1 else 0 end as table_exists`, sqlLiteral(tableName))
}
func (d MySQLDialect) CreateIndexSQL(indexName string, tableName string, columns []string) string {
	return fmt.Sprintf("create index %s\n  on %s(%s);", d.QuoteIdent(indexName), d.QuoteIdent(tableName), quoteColumnList(d, columns))
}

func (SQLiteDialect) Name() string       { return "sqlite" }
func (SQLiteDialect) DriverName() string { return "sqlite" }
func (SQLiteDialect) BindVar(int) string { return "?" }
func (SQLiteDialect) TextType() string   { return "text" }
func (SQLiteDialect) KeyTextType() string {
	return "text"
}
func (SQLiteDialect) JSONType() string { return "text" }
func (SQLiteDialect) TimeType() string { return "text" }
func (SQLiteDialect) BoolType() string { return "integer" }
func (SQLiteDialect) QuoteIdent(name string) string {
	return quoteDouble(name)
}
func (SQLiteDialect) UpsertClause(conflictColumn string, updateColumns []string) string {
	return standardUpsert(conflictColumn, updateColumns)
}
func (SQLiteDialect) TableExistsSQL(tableName string) string {
	return fmt.Sprintf(`select case when exists (
  select 1 from sqlite_master
  where type = 'table' and name = %s
) then 1 else 0 end as table_exists`, sqlLiteral(tableName))
}
func (d SQLiteDialect) CreateIndexSQL(indexName string, tableName string, columns []string) string {
	return standardCreateIndexSQL(d, indexName, tableName, columns)
}

func standardCreateIndexSQL(d Dialect, indexName string, tableName string, columns []string) string {
	return fmt.Sprintf("create index if not exists %s\n  on %s(%s);", d.QuoteIdent(indexName), d.QuoteIdent(tableName), quoteColumnList(d, columns))
}

func quoteColumnList(d Dialect, columns []string) string {
	quoted := make([]string, 0, len(columns))
	for _, column := range columns {
		column = strings.TrimSpace(column)
		if column == "" {
			continue
		}
		quoted = append(quoted, d.QuoteIdent(column))
	}
	return strings.Join(quoted, ", ")
}

func standardUpsert(conflictColumn string, updateColumns []string) string {
	assignments := make([]string, 0, len(updateColumns))
	for _, column := range updateColumns {
		column = strings.TrimSpace(column)
		if column == "" {
			continue
		}
		assignments = append(assignments, fmt.Sprintf("%s = excluded.%s", column, column))
	}
	return fmt.Sprintf("on conflict(%s) do update set %s", strings.TrimSpace(conflictColumn), strings.Join(assignments, ", "))
}

func quoteDouble(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

func sqlLiteral(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}
