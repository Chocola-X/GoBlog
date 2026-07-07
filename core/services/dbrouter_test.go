package services

import (
	"context"
	"database/sql"
	"testing"

	"goblog/core/models"

	_ "github.com/mattn/go-sqlite3"
)

func TestDBRouterUsesWriterForTransactions(t *testing.T) {
	writer, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	reader, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	ctx := context.Background()
	router := NewDBRouter(writer, reader)
	if _, err := router.ExecContext(ctx, `CREATE TABLE marker (id integer primary key, value text)`); err != nil {
		t.Fatal(err)
	}
	tx, err := router.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO marker (value) VALUES ('writer')`); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := writer.QueryRowContext(ctx, `SELECT COUNT(*) FROM marker`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("writer count = %d, want 1", count)
	}
	if err := reader.QueryRowContext(ctx, `SELECT COUNT(*) FROM marker`).Scan(&count); err == nil {
		t.Fatal("reader unexpectedly saw writer transaction table")
	}
}

func TestServicesUseReaderForReadsAndWriterForWrites(t *testing.T) {
	writer := newRouterTestDB(t)
	reader := newRouterTestDB(t)
	ctx := context.Background()
	if _, err := reader.ExecContext(ctx, `INSERT INTO gb_contents (title, slug, created, modified, text, type, status, allowComment, allowPing, allowFeed) VALUES ('Reader Post', 'reader-post', 1, 1, 'reader', 'post', 'publish', '1', '0', '1')`); err != nil {
		t.Fatal(err)
	}
	router := NewDBRouter(writer, reader, "sqlite")
	service := NewContentService(router)
	items, err := service.List(ctx, ContentQuery{Type: models.ContentTypePost, Status: "all", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Title != "Reader Post" {
		t.Fatalf("read did not use reader: %#v", items)
	}
	id, err := service.Create(ctx, SaveContentInput{Title: "Writer Post", Slug: "writer-post", Text: "writer", Type: models.ContentTypePost, Status: models.ContentStatusPost, AllowComment: true, AllowFeed: true}, 1)
	if err != nil {
		t.Fatal(err)
	}
	var title string
	if err := writer.QueryRowContext(ctx, `SELECT title FROM gb_contents WHERE cid = ?`, id).Scan(&title); err != nil {
		t.Fatal(err)
	}
	if title != "Writer Post" {
		t.Fatalf("writer title = %q", title)
	}
	var readerCount int
	if err := reader.QueryRowContext(ctx, `SELECT COUNT(*) FROM gb_contents WHERE slug = 'writer-post'`).Scan(&readerCount); err != nil {
		t.Fatal(err)
	}
	if readerCount != 0 {
		t.Fatalf("write leaked to reader, count=%d", readerCount)
	}
}

func newRouterTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := models.Migrate(context.Background(), db, "sqlite"); err != nil {
		t.Fatal(err)
	}
	return db
}
