package services

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"goblog/core/models"
	"goblog/core/plugin"
	"goblog/pkg/slug"
)

type ContentService struct {
	db *sql.DB
}

type SaveContentInput struct {
	Title  string
	Slug   string
	Text   string
	Status string
}

func NewContentService(db *sql.DB) *ContentService {
	return &ContentService{db: db}
}

func (s *ContentService) ListPublished(ctx context.Context, limit, offset int) ([]models.Content, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT cid, title, slug, created, modified, text, sortOrder, authorId, COALESCE(template,''), type, status,
			COALESCE(password,''), commentsNum, allowComment, allowPing, allowFeed, parent
		FROM gb_contents
		WHERE type = ? AND status = ?
		ORDER BY created DESC
		LIMIT ? OFFSET ?
	`, models.ContentTypePost, models.ContentStatusPost, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanContents(rows)
}

func (s *ContentService) ListPublishedPlugin(ctx context.Context, limit, offset int) ([]plugin.PublicContent, error) {
	contents, err := s.ListPublished(ctx, limit, offset)
	if err != nil {
		return nil, err
	}
	out := make([]plugin.PublicContent, 0, len(contents))
	for _, c := range contents {
		out = append(out, plugin.PublicContent{
			CID: c.CID, Title: c.Title, Slug: c.Slug, Created: c.Created,
			Modified: c.Modified, Text: c.Text, Type: c.Type, Status: c.Status,
		})
	}
	return out, nil
}

func (s *ContentService) ListAll(ctx context.Context, limit, offset int) ([]models.Content, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT cid, title, slug, created, modified, text, sortOrder, authorId, COALESCE(template,''), type, status,
			COALESCE(password,''), commentsNum, allowComment, allowPing, allowFeed, parent
		FROM gb_contents
		WHERE type = ?
		ORDER BY modified DESC
		LIMIT ? OFFSET ?
	`, models.ContentTypePost, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanContents(rows)
}

func (s *ContentService) Count(ctx context.Context) (int64, error) {
	var count int64
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM gb_contents WHERE type = ?`, models.ContentTypePost).Scan(&count)
	return count, err
}

func (s *ContentService) BySlug(ctx context.Context, postSlug string) (models.Content, error) {
	return s.one(ctx, `WHERE slug = ? AND type = ? AND status = ?`, postSlug, models.ContentTypePost, models.ContentStatusPost)
}

func (s *ContentService) ByID(ctx context.Context, id int64) (models.Content, error) {
	return s.one(ctx, `WHERE cid = ?`, id)
}

func (s *ContentService) Create(ctx context.Context, input SaveContentInput, authorID int64) (int64, error) {
	now := time.Now().Unix()
	postSlug, err := s.uniqueSlug(ctx, input.Slug, input.Title, 0)
	if err != nil {
		return 0, err
	}
	status := normalizeStatus(input.Status)
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO gb_contents (title, slug, created, modified, text, sortOrder, authorId, template, type, status, password, commentsNum, allowComment, allowPing, allowFeed, parent)
		VALUES (?, ?, ?, ?, ?, 0, ?, '', ?, ?, '', 0, '1', '0', '1', 0)
	`, input.Title, postSlug, now, now, input.Text, authorID, models.ContentTypePost, status)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *ContentService) Update(ctx context.Context, id int64, input SaveContentInput) error {
	postSlug, err := s.uniqueSlug(ctx, input.Slug, input.Title, id)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `
		UPDATE gb_contents
		SET title = ?, slug = ?, modified = ?, text = ?, status = ?
		WHERE cid = ?
	`, input.Title, postSlug, time.Now().Unix(), input.Text, normalizeStatus(input.Status), id)
	return err
}

func (s *ContentService) Delete(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM gb_contents WHERE cid = ?`, id)
	return err
}

func (s *ContentService) one(ctx context.Context, where string, args ...any) (models.Content, error) {
	query := `
		SELECT cid, title, slug, created, modified, text, sortOrder, authorId, COALESCE(template,''), type, status,
			COALESCE(password,''), commentsNum, allowComment, allowPing, allowFeed, parent
		FROM gb_contents ` + where + ` LIMIT 1`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return models.Content{}, err
	}
	defer rows.Close()
	contents, err := scanContents(rows)
	if err != nil {
		return models.Content{}, err
	}
	if len(contents) == 0 {
		return models.Content{}, sql.ErrNoRows
	}
	return contents[0], nil
}

func (s *ContentService) uniqueSlug(ctx context.Context, raw, title string, exceptID int64) (string, error) {
	base := slug.Make(raw)
	if raw == "" {
		base = slug.Make(title)
	}

	for i := 0; i < 1000; i++ {
		candidate := base
		if i > 0 {
			candidate = fmt.Sprintf("%s-%d", base, i+1)
		}

		var id int64
		err := s.db.QueryRowContext(ctx, `SELECT cid FROM gb_contents WHERE slug = ? LIMIT 1`, candidate).Scan(&id)
		if errors.Is(err, sql.ErrNoRows) || (err == nil && id == exceptID) {
			return candidate, nil
		}
		if err != nil {
			return "", err
		}
	}

	return "", errors.New("cannot allocate unique slug")
}

func scanContents(rows *sql.Rows) ([]models.Content, error) {
	var contents []models.Content
	for rows.Next() {
		var c models.Content
		if err := rows.Scan(&c.CID, &c.Title, &c.Slug, &c.Created, &c.Modified, &c.Text, &c.SortOrder, &c.AuthorID, &c.Template, &c.Type, &c.Status, &c.Password, &c.CommentsNum, &c.AllowComment, &c.AllowPing, &c.AllowFeed, &c.Parent); err != nil {
			return nil, err
		}
		contents = append(contents, c)
	}
	return contents, rows.Err()
}

func normalizeStatus(status string) string {
	if status == models.ContentStatusDraft {
		return models.ContentStatusDraft
	}
	return models.ContentStatusPost
}
