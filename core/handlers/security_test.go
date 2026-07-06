package handlers

import (
	"bytes"
	"context"
	"database/sql"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"goblog/core/models"
	"goblog/core/plugin"
	"goblog/core/services"
	"goblog/pkg/auth"

	_ "github.com/mattn/go-sqlite3"
)

func TestSafeNextRejectsExternalURL(t *testing.T) {
	for _, input := range []string{"http://evil.example", "https://evil.example", "//evil.example/path", "admin"} {
		if got := safeNext(input); got != "" {
			t.Fatalf("safeNext(%q) = %q, want empty", input, got)
		}
	}
	if got := safeNext("/admin/posts"); got != "/admin/posts" {
		t.Fatalf("safeNext relative path = %q", got)
	}
}

func TestCSRFTokensBindSubjectAndPurpose(t *testing.T) {
	now := time.Date(2026, 7, 6, 0, 0, 0, 0, time.UTC)
	admin := signCSRF("secret", "1", "admin", now)
	if admin == signCSRF("secret", "2", "admin", now) {
		t.Fatal("csrf token should differ by subject")
	}
	if admin == signCSRF("secret", "1", "comment", now) {
		t.Fatal("csrf token should differ by purpose")
	}
}

func TestAdminPostRequiresCSRF(t *testing.T) {
	app, secret, adminID := newSecurityTestApp(t)
	req := httptest.NewRequest(http.MethodPost, "/admin/logout", nil)
	setSession(t, req, secret, adminID)
	rec := httptest.NewRecorder()

	app.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("POST without csrf status = %d, want 403", rec.Code)
	}
}

func TestAdminPostRejectsWrongCSRF(t *testing.T) {
	app, secret, adminID := newSecurityTestApp(t)
	form := url.Values{"_csrf": {"wrong"}}
	req := httptest.NewRequest(http.MethodPost, "/admin/logout", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	setSession(t, req, secret, adminID)
	rec := httptest.NewRecorder()

	app.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("POST with wrong csrf status = %d, want 403", rec.Code)
	}
}

func TestLoginNextCannotRedirectOffsite(t *testing.T) {
	app, _, _ := newSecurityTestApp(t)
	form := url.Values{
		"name":     {"admin"},
		"password": {"admin123"},
		"next":     {"http://evil.example"},
	}
	req := httptest.NewRequest(http.MethodPost, "/admin/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	form.Set("_csrf", app.csrfTokenFor(req, "login"))
	req = httptest.NewRequest(http.MethodPost, "/admin/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()

	app.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("login status = %d, want 303", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/admin" {
		t.Fatalf("login redirect = %q, want /admin", loc)
	}
}

func TestPermissionMatrixAndAuthorBoundary(t *testing.T) {
	app, secret, adminID := newSecurityTestApp(t)
	ctx := context.Background()
	contributorID, err := app.Users.Save(ctx, services.SaveUserInput{Name: "contrib", Password: "secret123", Mail: "c@example.com", Role: "contributor"}, 0)
	if err != nil {
		t.Fatal(err)
	}
	editorID, err := app.Users.Save(ctx, services.SaveUserInput{Name: "editor", Password: "secret123", Mail: "e@example.com", Role: "editor"}, 0)
	if err != nil {
		t.Fatal(err)
	}
	subscriberID, err := app.Users.Save(ctx, services.SaveUserInput{Name: "sub", Password: "secret123", Mail: "s@example.com", Role: "subscriber"}, 0)
	if err != nil {
		t.Fatal(err)
	}
	postID, err := app.Contents.Create(ctx, services.SaveContentInput{Title: "Admin Post", Type: models.ContentTypePost, Status: models.ContentStatusPost, AllowComment: true}, adminID)
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/admin/users", nil)
	setSession(t, req, secret, contributorID)
	rec := httptest.NewRecorder()
	app.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("contributor /admin/users status = %d, want 403", rec.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "/admin/posts/"+itoa(postID)+"/edit", nil)
	setSession(t, req, secret, contributorID)
	rec = httptest.NewRecorder()
	app.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("contributor editing another author's post status = %d, want 403", rec.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "/admin/options/general", nil)
	setSession(t, req, secret, editorID)
	rec = httptest.NewRecorder()
	app.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("editor /admin/options/general status = %d, want 403", rec.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "/admin", nil)
	setSession(t, req, secret, subscriberID)
	rec = httptest.NewRecorder()
	app.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("subscriber /admin status = %d, want 403", rec.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "/admin/profile", nil)
	setSession(t, req, secret, subscriberID)
	rec = httptest.NewRecorder()
	app.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("subscriber /admin/profile status = %d, want 200", rec.Code)
	}
}

func TestContentRoutesRejectTypeMismatch(t *testing.T) {
	app, secret, _ := newSecurityTestApp(t)
	ctx := context.Background()
	contributorID, err := app.Users.Save(ctx, services.SaveUserInput{Name: "contrib", Password: "secret123", Mail: "c@example.com", Role: "contributor"}, 0)
	if err != nil {
		t.Fatal(err)
	}
	pageID, err := app.Contents.Create(ctx, services.SaveContentInput{Title: "Own Page", Type: models.ContentTypePage, Status: models.ContentStatusPost}, contributorID)
	if err != nil {
		t.Fatal(err)
	}
	attachmentID, err := app.Contents.CreateAttachment(ctx, "asset.txt", "asset", "/uploads/asset.txt", contributorID, 0)
	if err != nil {
		t.Fatal(err)
	}

	for _, id := range []int64{pageID, attachmentID} {
		req := httptest.NewRequest(http.MethodGet, "/admin/posts/"+itoa(id)+"/edit", nil)
		setSession(t, req, secret, contributorID)
		rec := httptest.NewRecorder()
		app.Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("cross-type /admin/posts/%d/edit status = %d, want 404", id, rec.Code)
		}
	}
}

func TestContributorAttachmentUploadsMustTargetOwnPost(t *testing.T) {
	app, secret, adminID := newSecurityTestApp(t)
	app.UploadDir = t.TempDir()
	ctx := context.Background()
	contributorID, err := app.Users.Save(ctx, services.SaveUserInput{Name: "contrib", Password: "secret123", Mail: "c@example.com", Role: "contributor"}, 0)
	if err != nil {
		t.Fatal(err)
	}
	ownPostID, err := app.Contents.Create(ctx, services.SaveContentInput{Title: "Own Post", Type: models.ContentTypePost, Status: models.ContentStatusPost}, contributorID)
	if err != nil {
		t.Fatal(err)
	}
	otherPostID, err := app.Contents.Create(ctx, services.SaveContentInput{Title: "Other Post", Type: models.ContentTypePost, Status: models.ContentStatusPost}, adminID)
	if err != nil {
		t.Fatal(err)
	}

	req := multipartUploadRequest(t, "/admin/medias", map[string]string{"_csrf": adminToken(secret, contributorID)}, "file", "a.txt", "hello")
	setSession(t, req, secret, contributorID)
	rec := httptest.NewRecorder()
	app.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("contributor upload without cid status = %d, want 403", rec.Code)
	}

	req = multipartUploadRequest(t, "/admin/medias", map[string]string{"_csrf": adminToken(secret, contributorID), "cid": itoa(otherPostID)}, "file", "a.txt", "hello")
	setSession(t, req, secret, contributorID)
	rec = httptest.NewRecorder()
	app.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("contributor upload to another post status = %d, want 403", rec.Code)
	}

	req = multipartUploadRequest(t, "/admin/medias", map[string]string{"_csrf": adminToken(secret, contributorID), "cid": itoa(ownPostID)}, "file", "a.txt", "hello")
	setSession(t, req, secret, contributorID)
	rec = httptest.NewRecorder()
	app.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("contributor upload to own post status = %d, want 303", rec.Code)
	}
}

func newSecurityTestApp(t *testing.T) (*App, string, int64) {
	t.Helper()
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
	options := services.NewOptionService(db)
	if err := options.EnsureDefaults(ctx); err != nil {
		t.Fatal(err)
	}
	users := services.NewUserService(db)
	if err := users.EnsureDefaultAdmin(ctx, "admin", "admin123", "admin@example.com"); err != nil {
		t.Fatal(err)
	}
	metas := services.NewMetaService(db)
	if err := metas.EnsureDefaultCategory(ctx); err != nil {
		t.Fatal(err)
	}
	admin, err := users.ByName(ctx, "admin")
	if err != nil {
		t.Fatal(err)
	}
	secret, err := options.Get(ctx, "auth_secret")
	if err != nil {
		t.Fatal(err)
	}
	return New(services.NewContentService(db), metas, services.NewCommentService(db), users, options, plugin.Default), secret, admin.UID
}

func setSession(t *testing.T, req *http.Request, secret string, uid int64) {
	t.Helper()
	rec := httptest.NewRecorder()
	auth.SetSession(rec, secret, uid)
	for _, cookie := range rec.Result().Cookies() {
		req.AddCookie(cookie)
	}
}

func multipartUploadRequest(t *testing.T, target string, fields map[string]string, fileField, filename, content string) *http.Request {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for key, value := range fields {
		if err := writer.WriteField(key, value); err != nil {
			t.Fatal(err)
		}
	}
	part, err := writer.CreateFormFile(fileField, filename)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write([]byte(content)); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, target, &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	return req
}

func adminToken(secret string, uid int64) string {
	return signCSRF(secret, strconv.FormatInt(uid, 10), "admin", time.Now().UTC())
}

func itoa(v int64) string {
	return strconv.FormatInt(v, 10)
}
