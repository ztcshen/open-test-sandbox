package sqlstore

import (
	"database/sql"
	"strings"
)

type Store struct {
	db      *sql.DB
	dialect Dialect
}

func New(db *sql.DB, dialect Dialect) *Store {
	return &Store{db: db, dialect: dialect}
}

func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *Store) bindVars(count int) string {
	vars := make([]string, 0, count)
	for i := 1; i <= count; i++ {
		vars = append(vars, s.dialect.BindVar(i))
	}
	return strings.Join(vars, ", ")
}
