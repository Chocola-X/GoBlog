package models

import (
	"context"
	"database/sql"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

func TestRunVersionedMigrationsReturnsUnexpectedErrors(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	if _, err := db.ExecContext(ctx, `CREATE TABLE gb_options (name varchar(64) NOT NULL, user int(10) NOT NULL default '0', value text, PRIMARY KEY (name, user))`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO gb_options (name, user, value) VALUES ('schema_version', 0, '0')`); err != nil {
		t.Fatal(err)
	}
	if err := RunVersionedMigrations(ctx, db); err == nil {
		t.Fatal("expected migration error when content/user tables are missing")
	}
}
