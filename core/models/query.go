package models

import (
	"fmt"
	"strings"
)

type Dialect string

const (
	DialectSQLite   Dialect = "sqlite"
	DialectMySQL    Dialect = "mysql"
	DialectPostgres Dialect = "postgres"
)

func NormalizeDialect(driver string) Dialect {
	switch strings.ToLower(driver) {
	case "mysql", "mariadb":
		return DialectMySQL
	case "postgres", "postgresql", "pgx":
		return DialectPostgres
	default:
		return DialectSQLite
	}
}

func Rebind(d Dialect, query string) string {
	if d != DialectPostgres {
		return query
	}
	var b strings.Builder
	index := 1
	for i := 0; i < len(query); i++ {
		if query[i] == '?' {
			b.WriteString(fmt.Sprintf("$%d", index))
			index++
			continue
		}
		b.WriteByte(query[i])
	}
	return b.String()
}

func UpsertOptionSQL(d Dialect) string {
	if d == DialectMySQL {
		return `INSERT INTO gb_options (name, user, value) VALUES (?, ?, ?) ON DUPLICATE KEY UPDATE value = VALUES(value)`
	}
	if d == DialectPostgres {
		return `INSERT INTO gb_options (name, "user", value) VALUES ($1, $2, $3) ON CONFLICT(name, "user") DO UPDATE SET value = EXCLUDED.value`
	}
	return `INSERT INTO gb_options (name, user, value) VALUES (?, ?, ?) ON CONFLICT(name, user) DO UPDATE SET value = excluded.value`
}

func LimitOffset(d Dialect, limit, offset int) (string, []any) {
	if limit <= 0 {
		return "", nil
	}
	return " LIMIT ? OFFSET ?", []any{limit, offset}
}
