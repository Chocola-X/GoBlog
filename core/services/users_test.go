package services

import (
	"context"
	"database/sql"
	"testing"

	"goblog/core/models"

	_ "github.com/mattn/go-sqlite3"
)

func TestUserSessionRevisionAndLastAdministratorProtection(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	ctx := context.Background()
	if err := models.Migrate(ctx, db, "sqlite"); err != nil {
		t.Fatal(err)
	}
	users := NewUserService(db)
	if err := users.EnsureDefaultAdmin(ctx, "admin", "admin123", "admin@example.com"); err != nil {
		t.Fatal(err)
	}
	admin, err := users.ByName(ctx, "admin")
	if err != nil {
		t.Fatal(err)
	}
	visitorID, err := users.Save(ctx, SaveUserInput{Name: "visitor", Password: "visitor123", Mail: "visitor@example.com", Role: "visitor"}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := users.Delete(ctx, admin.UID); err == nil {
		t.Fatal("deleting the last administrator succeeded")
	}
	if err := users.RevokeSessions(ctx, visitorID); err != nil {
		t.Fatal(err)
	}
	visitor, err := users.ByID(ctx, visitorID)
	if err != nil {
		t.Fatal(err)
	}
	if visitor.AuthCode == "" {
		t.Fatal("revoking sessions did not rotate authCode")
	}
	previousCode := visitor.AuthCode
	if err := users.ChangePassword(ctx, visitorID, "changed123"); err != nil {
		t.Fatal(err)
	}
	visitor, err = users.ByID(ctx, visitorID)
	if err != nil {
		t.Fatal(err)
	}
	if visitor.AuthCode == "" || visitor.AuthCode == previousCode {
		t.Fatal("changing password did not rotate authCode")
	}
}
