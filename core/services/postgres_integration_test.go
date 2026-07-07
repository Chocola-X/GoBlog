package services

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"
	"time"

	"goblog/core/models"

	"github.com/lib/pq"
)

func TestPostgresCMSFlowSmoke(t *testing.T) {
	dsn := os.Getenv("GOBLOG_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("set GOBLOG_POSTGRES_TEST_DSN to run PostgreSQL integration smoke test")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	ctx := context.Background()
	schema := fmt.Sprintf("goblog_phase08_%d", time.Now().UnixNano())
	if _, err := db.ExecContext(ctx, `CREATE SCHEMA `+pq.QuoteIdentifier(schema)); err != nil {
		t.Fatal(err)
	}
	defer db.ExecContext(ctx, `DROP SCHEMA IF EXISTS `+pq.QuoteIdentifier(schema)+` CASCADE`)
	if _, err := db.ExecContext(ctx, `SET search_path TO `+pq.QuoteIdentifier(schema)); err != nil {
		t.Fatal(err)
	}
	if err := models.Migrate(ctx, db, "postgres"); err != nil {
		t.Fatal(err)
	}
	serviceDB := NewSQLDB(db, "postgres")
	options := NewOptionService(serviceDB)
	if err := options.EnsureDefaults(ctx); err != nil {
		t.Fatal(err)
	}
	users := NewUserService(serviceDB)
	if err := users.EnsureDefaultAdmin(ctx, "admin", "admin123", "admin@example.com"); err != nil {
		t.Fatal(err)
	}
	admin, err := users.ByName(ctx, "admin")
	if err != nil {
		t.Fatal(err)
	}
	metas := NewMetaService(serviceDB)
	if err := metas.EnsureDefaultCategory(ctx); err != nil {
		t.Fatal(err)
	}
	contents := NewContentService(serviceDB)
	postID, err := contents.Create(ctx, SaveContentInput{Title: "Postgres Post", Slug: "pg-post", Text: "body", Type: models.ContentTypePost, Status: models.ContentStatusPost, AllowComment: true, AllowPing: true, AllowFeed: true}, admin.UID)
	if err != nil {
		t.Fatal(err)
	}
	comments := NewCommentService(serviceDB)
	if err := comments.Save(ctx, SaveCommentInput{CID: postID, Author: "Tester", Text: "hello", Type: "comment", Status: "approved"}, 0); err != nil {
		t.Fatal(err)
	}
	posts, err := contents.List(ctx, ContentQuery{Type: models.ContentTypePost, Status: "all", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(posts) != 1 || posts[0].CID != postID {
		t.Fatalf("posts = %#v, want cid %d", posts, postID)
	}
	list, err := comments.ListFiltered(ctx, "all", "", postID, "comment")
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("comments = %#v", list)
	}
}
