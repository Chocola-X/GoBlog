package services

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"goblog/core/models"

	_ "github.com/mattn/go-sqlite3"
)

func TestMetaHierarchySortAndDeletePromotion(t *testing.T) {
	db, metas := newMetaTestService(t)
	ctx := context.Background()
	rootID := saveTestMeta(t, metas, SaveMetaInput{Name: "Root", Type: "category"})
	secondID := saveTestMeta(t, metas, SaveMetaInput{Name: "Second", Type: "category"})
	childID := saveTestMeta(t, metas, SaveMetaInput{Name: "Child", Type: "category", Parent: rootID})
	grandchildID := saveTestMeta(t, metas, SaveMetaInput{Name: "Grandchild", Type: "category", Parent: childID})

	root, _ := metas.ByID(ctx, rootID)
	second, _ := metas.ByID(ctx, secondID)
	if root.SortOrder != 1 || second.SortOrder != 2 {
		t.Fatalf("root sort orders = %d, %d, want 1, 2", root.SortOrder, second.SortOrder)
	}
	if _, err := metas.Save(ctx, SaveMetaInput{Name: root.Name, Slug: root.Slug, Type: "category", Parent: grandchildID}, rootID); err == nil {
		t.Fatal("moving a category below its descendant succeeded")
	}
	if _, err := db.Exec(`UPDATE gb_metas SET sortOrder = 0 WHERE parent = 0`); err != nil {
		t.Fatal(err)
	}
	if err := metas.Move(ctx, secondID, "up"); err != nil {
		t.Fatal(err)
	}
	items, err := metas.List(ctx, "category")
	if err != nil {
		t.Fatal(err)
	}
	var roots []models.Meta
	for _, item := range items {
		if item.Parent == 0 {
			roots = append(roots, item)
		}
	}
	if len(roots) < 2 || roots[0].MID != secondID || roots[1].MID != rootID {
		t.Fatalf("root order after move = %#v", roots)
	}
	if roots[0].SortOrder != 1 || roots[1].SortOrder != 2 {
		t.Fatalf("normalized root sort orders = %d, %d", roots[0].SortOrder, roots[1].SortOrder)
	}

	if err := metas.Delete(ctx, childID); err != nil {
		t.Fatal(err)
	}
	grandchild, err := metas.ByID(ctx, grandchildID)
	if err != nil {
		t.Fatal(err)
	}
	if grandchild.Parent != rootID {
		t.Fatalf("grandchild parent = %d, want promoted to %d", grandchild.Parent, rootID)
	}
	var relationships int
	if err := db.QueryRow(`SELECT COUNT(*) FROM gb_relationships WHERE mid = ?`, childID).Scan(&relationships); err != nil {
		t.Fatal(err)
	}
	if relationships != 0 {
		t.Fatalf("deleted category relationships = %d, want 0", relationships)
	}
}

func TestMetaMergeMovesRelationshipsAndChildren(t *testing.T) {
	db, metas := newMetaTestService(t)
	ctx := context.Background()
	targetID := saveTestMeta(t, metas, SaveMetaInput{Name: "Target", Type: "category"})
	sourceID := saveTestMeta(t, metas, SaveMetaInput{Name: "Source", Type: "category"})
	childID := saveTestMeta(t, metas, SaveMetaInput{Name: "Child", Type: "category", Parent: sourceID})

	contents := NewContentService(db)
	contentID, err := contents.Create(ctx, SaveContentInput{
		Title: "Post", Type: models.ContentTypePost, Status: models.ContentStatusPost,
		CategoryIDs: []int64{targetID, sourceID},
	}, 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := metas.Merge(ctx, childID, []int64{sourceID}, "category"); err == nil {
		t.Fatal("merging a category into its descendant succeeded")
	}
	if err := metas.Merge(ctx, targetID, []int64{sourceID}, "category"); err != nil {
		t.Fatal(err)
	}
	if _, err := metas.ByID(ctx, sourceID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("merged source error = %v, want sql.ErrNoRows", err)
	}
	child, err := metas.ByID(ctx, childID)
	if err != nil {
		t.Fatal(err)
	}
	if child.Parent != targetID {
		t.Fatalf("merged child parent = %d, want %d", child.Parent, targetID)
	}
	var relationships int
	if err := db.QueryRow(`SELECT COUNT(*) FROM gb_relationships WHERE cid = ? AND mid = ?`, contentID, targetID).Scan(&relationships); err != nil {
		t.Fatal(err)
	}
	if relationships != 1 {
		t.Fatalf("merged relationships = %d, want one deduplicated row", relationships)
	}
	target, err := metas.ByID(ctx, targetID)
	if err != nil {
		t.Fatal(err)
	}
	if target.Count != 1 {
		t.Fatalf("merged target count = %d, want 1", target.Count)
	}
}

func TestCleanOrphanTags(t *testing.T) {
	db, metas := newMetaTestService(t)
	ctx := context.Background()
	orphanID := saveTestMeta(t, metas, SaveMetaInput{Name: "Orphan", Type: "tag"})
	usedID := saveTestMeta(t, metas, SaveMetaInput{Name: "Used", Type: "tag"})
	if _, err := db.Exec(`INSERT INTO gb_relationships (cid, mid) VALUES (?, ?)`, 99, usedID); err != nil {
		t.Fatal(err)
	}
	cleaned, err := metas.CleanOrphanTags(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if cleaned != 1 {
		t.Fatalf("cleaned tags = %d, want 1", cleaned)
	}
	if _, err := metas.ByID(ctx, orphanID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("orphan lookup error = %v, want sql.ErrNoRows", err)
	}
	if _, err := metas.ByID(ctx, usedID); err != nil {
		t.Fatalf("used tag was deleted: %v", err)
	}
}

func newMetaTestService(t *testing.T) (*sql.DB, *MetaService) {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	if err := models.Migrate(context.Background(), db, "sqlite"); err != nil {
		t.Fatal(err)
	}
	return db, NewMetaService(db)
}

func saveTestMeta(t *testing.T, metas *MetaService, input SaveMetaInput) int64 {
	t.Helper()
	id, err := metas.Save(context.Background(), input, 0)
	if err != nil {
		t.Fatal(err)
	}
	return id
}
