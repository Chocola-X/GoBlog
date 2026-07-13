package services

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"goblog/core/models"
)

type CommentService struct {
	db DB
}

type SaveCommentInput struct {
	CID      int64
	Author   string
	AuthorID int64
	OwnerID  int64
	Mail     string
	URL      string
	Text     string
	Status   string
	Parent   int64
	IP       string
	Agent    string
	Type     string
}

func NewCommentService(db any) *CommentService {
	return &CommentService{db: WrapDB(db)}
}

func (s *CommentService) List(ctx context.Context, status, keywords string, cid int64) ([]models.Comment, error) {
	return s.ListFiltered(ctx, status, keywords, cid, "")
}

func (s *CommentService) ListFiltered(ctx context.Context, status, keywords string, cid int64, typ string) ([]models.Comment, error) {
	if status == "" {
		status = "approved"
	}
	var args []any
	var where []string
	if status != "all" {
		args = append(args, status)
		where = append(where, "cm.status = ?")
	}
	if cid > 0 {
		where = append(where, "cm.cid = ?")
		args = append(args, cid)
	}
	if typ != "" && typ != "all" {
		where = append(where, "cm.type = ?")
		args = append(args, typ)
	}
	if keywords != "" {
		where = append(where, "(cm.author LIKE ? OR cm.mail LIKE ? OR cm.text LIKE ?)")
		kw := "%" + keywords + "%"
		args = append(args, kw, kw, kw)
	}
	if len(where) == 0 {
		where = append(where, "1 = 1")
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT cm.coid, cm.cid, cm.created, COALESCE(cm.author,''), cm.authorId, cm.ownerId, COALESCE(cm.mail,''), COALESCE(cm.url,''), COALESCE(cm.ip,''), COALESCE(cm.agent,''), COALESCE(cm.text,''), cm.type, cm.status, cm.parent,
			COALESCE(c.title,''), COALESCE(c.slug,'')
		FROM gb_comments cm LEFT JOIN gb_contents c ON c.cid = cm.cid
		WHERE `+strings.Join(where, " AND ")+`
		ORDER BY cm.created DESC LIMIT 200
	`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var comments []models.Comment
	for rows.Next() {
		var c models.Comment
		if err := rows.Scan(&c.COID, &c.CID, &c.Created, &c.Author, &c.AuthorID, &c.OwnerID, &c.Mail, &c.URL, &c.IP, &c.Agent, &c.Text, &c.Type, &c.Status, &c.Parent, &c.Title, &c.Slug); err != nil {
			return nil, err
		}
		comments = append(comments, c)
	}
	return comments, rows.Err()
}

func (s *CommentService) ExistsByURLType(ctx context.Context, cid int64, commentURL, typ string) (bool, error) {
	if cid <= 0 || commentURL == "" || typ == "" {
		return false, nil
	}
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM gb_comments WHERE cid = ? AND url = ? AND type = ?`, cid, commentURL, typ).Scan(&count)
	return count > 0, err
}

func (s *CommentService) ListForContent(ctx context.Context, cid int64, order string, limit, offset int) ([]models.Comment, error) {
	if strings.ToUpper(order) != "DESC" {
		order = "ASC"
	} else {
		order = "DESC"
	}
	query := `
		SELECT coid, cid, created, COALESCE(author,''), authorId, ownerId, COALESCE(mail,''), COALESCE(url,''), COALESCE(ip,''), COALESCE(agent,''), COALESCE(text,''), type, status, parent
		FROM gb_comments WHERE cid = ? AND status = 'approved' ORDER BY created ` + order + `, coid ` + order
	args := []any{cid}
	if limit > 0 {
		query += ` LIMIT ? OFFSET ?`
		args = append(args, limit, offset)
	}
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var comments []models.Comment
	for rows.Next() {
		var c models.Comment
		if err := rows.Scan(&c.COID, &c.CID, &c.Created, &c.Author, &c.AuthorID, &c.OwnerID, &c.Mail, &c.URL, &c.IP, &c.Agent, &c.Text, &c.Type, &c.Status, &c.Parent); err != nil {
			return nil, err
		}
		comments = append(comments, c)
	}
	return comments, rows.Err()
}

func (s *CommentService) CountForContent(ctx context.Context, cid int64) (int64, error) {
	var count int64
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM gb_comments WHERE cid = ? AND status = 'approved'`, cid).Scan(&count)
	return count, err
}

func (s *CommentService) CountRecentByIP(ctx context.Context, ip string, since int64) (int64, error) {
	return s.CountRecentByIPForContent(ctx, 0, ip, since)
}

func (s *CommentService) CountRecentByIPForContent(ctx context.Context, cid int64, ip string, since int64) (int64, error) {
	if ip == "" {
		return 0, nil
	}
	var count int64
	query := `SELECT COUNT(*) FROM gb_comments WHERE ip = ? AND created >= ?`
	args := []any{ip, since}
	if cid > 0 {
		query += ` AND cid = ?`
		args = append(args, cid)
	}
	err := s.db.QueryRowContext(ctx, query, args...).Scan(&count)
	return count, err
}

func (s *CommentService) ByID(ctx context.Context, id int64) (models.Comment, error) {
	var c models.Comment
	err := s.db.QueryRowContext(ctx, `
		SELECT coid, cid, created, COALESCE(author,''), authorId, ownerId, COALESCE(mail,''), COALESCE(url,''), COALESCE(ip,''), COALESCE(agent,''), COALESCE(text,''), type, status, parent
		FROM gb_comments WHERE coid = ?
	`, id).Scan(&c.COID, &c.CID, &c.Created, &c.Author, &c.AuthorID, &c.OwnerID, &c.Mail, &c.URL, &c.IP, &c.Agent, &c.Text, &c.Type, &c.Status, &c.Parent)
	return c, err
}

func (s *CommentService) HasApprovedAuthor(ctx context.Context, author, mail string) (bool, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM gb_comments WHERE author = ? AND mail = ? AND status = 'approved'
	`, author, mail).Scan(&count)
	return count > 0, err
}

func (s *CommentService) ParentDepth(ctx context.Context, cid, parent int64) (int, error) {
	if parent <= 0 {
		return 0, nil
	}
	depth := 0
	seen := map[int64]bool{}
	for parent > 0 {
		if seen[parent] {
			return depth, nil
		}
		seen[parent] = true
		var next, parentCID int64
		err := s.db.QueryRowContext(ctx, `SELECT parent, cid FROM gb_comments WHERE coid = ?`, parent).Scan(&next, &parentCID)
		if err != nil {
			return depth, err
		}
		if parentCID != cid {
			return depth, sql.ErrNoRows
		}
		depth++
		parent = next
	}
	return depth, nil
}

func (s *CommentService) Save(ctx context.Context, input SaveCommentInput, id int64) error {
	ctx = WithWriter(ctx)
	status := input.Status
	if status == "" {
		status = "approved"
	}
	typ := strings.TrimSpace(input.Type)
	if typ == "" {
		typ = "comment"
	}
	if id > 0 {
		var cid int64
		_ = s.db.QueryRowContext(ctx, `SELECT cid FROM gb_comments WHERE coid = ?`, id).Scan(&cid)
		_, err := s.db.ExecContext(ctx, `UPDATE gb_comments SET author = ?, mail = ?, url = ?, text = ?, status = ?, type = ? WHERE coid = ?`, input.Author, input.Mail, input.URL, input.Text, status, typ, id)
		if err == nil && cid > 0 {
			err = s.refreshContentCount(ctx, cid)
		}
		return err
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO gb_comments (cid, created, author, authorId, ownerId, mail, url, ip, agent, text, type, status, parent)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, input.CID, time.Now().Unix(), input.Author, input.AuthorID, input.OwnerID, input.Mail, input.URL, input.IP, input.Agent, input.Text, typ, status, input.Parent)
	if err != nil {
		return err
	}
	return s.refreshContentCount(ctx, input.CID)
}

func (s *CommentService) Mark(ctx context.Context, id int64, status string) error {
	ctx = WithWriter(ctx)
	var cid int64
	_ = s.db.QueryRowContext(ctx, `SELECT cid FROM gb_comments WHERE coid = ?`, id).Scan(&cid)
	_, err := s.db.ExecContext(ctx, `UPDATE gb_comments SET status = ? WHERE coid = ?`, status, id)
	if err == nil && cid > 0 {
		err = s.refreshContentCount(ctx, cid)
	}
	return err
}

func (s *CommentService) MarkMany(ctx context.Context, ids []int64, status string) error {
	ctx = WithWriter(ctx)
	for _, id := range ids {
		if id <= 0 {
			continue
		}
		if err := s.Mark(ctx, id, status); err != nil {
			return err
		}
	}
	return s.refreshAllContentCounts(ctx)
}

func (s *CommentService) Delete(ctx context.Context, id int64) error {
	ctx = WithWriter(ctx)
	var cid int64
	_ = s.db.QueryRowContext(ctx, `SELECT cid FROM gb_comments WHERE coid = ?`, id).Scan(&cid)
	_, err := s.db.ExecContext(ctx, `DELETE FROM gb_comments WHERE coid = ?`, id)
	if err == nil && cid > 0 {
		err = s.refreshContentCount(ctx, cid)
	}
	return err
}

func (s *CommentService) DeleteMany(ctx context.Context, ids []int64) error {
	ctx = WithWriter(ctx)
	for _, id := range ids {
		if id <= 0 {
			continue
		}
		if _, err := s.db.ExecContext(ctx, `DELETE FROM gb_comments WHERE coid = ?`, id); err != nil {
			return err
		}
	}
	return s.refreshAllContentCounts(ctx)
}

func (s *CommentService) ClearSpam(ctx context.Context) error {
	ctx = WithWriter(ctx)
	if _, err := s.db.ExecContext(ctx, `DELETE FROM gb_comments WHERE status = 'spam'`); err != nil {
		return err
	}
	return s.refreshAllContentCounts(ctx)
}

func (s *CommentService) refreshContentCount(ctx context.Context, cid int64) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE gb_contents SET commentsNum = (
			SELECT COUNT(*) FROM gb_comments WHERE cid = ? AND status = 'approved'
		) WHERE cid = ?
	`, cid, cid)
	return err
}

func (s *CommentService) refreshAllContentCounts(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE gb_contents SET commentsNum = (
			SELECT COUNT(*) FROM gb_comments WHERE gb_comments.cid = gb_contents.cid AND status = 'approved'
		)
	`)
	return err
}
