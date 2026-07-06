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
	_ "goblog/themes/default"
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

func TestDraftPreviewRequiresSignedToken(t *testing.T) {
	app, _, adminID := newSecurityTestApp(t)
	ctx := context.Background()
	id, err := app.Contents.Create(ctx, services.SaveContentInput{Title: "Draft Preview", Slug: "draft-preview", Text: "draft body", Type: models.ContentTypePost, Status: models.ContentStatusDraft}, adminID)
	if err != nil {
		t.Fatal(err)
	}
	item, err := app.Contents.ByID(ctx, id)
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/preview/"+itoa(id), nil)
	rec := httptest.NewRecorder()
	app.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("preview without token status = %d, want 403", rec.Code)
	}

	req = httptest.NewRequest(http.MethodGet, app.previewURL(req, item), nil)
	rec = httptest.NewRecorder()
	app.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("preview with token status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
}

func TestFuturePostDoesNotLeakInSearchPage(t *testing.T) {
	app, _, adminID := newSecurityTestApp(t)
	ctx := context.Background()
	_, err := app.Contents.Create(ctx, services.SaveContentInput{
		Title:   "Future Secret",
		Slug:    "future-secret",
		Text:    "hidden",
		Type:    models.ContentTypePost,
		Status:  models.ContentStatusPost,
		Created: time.Now().Add(24 * time.Hour).Unix(),
	}, adminID)
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/search?q=Future", nil)
	rec := httptest.NewRecorder()
	app.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("search status = %d, want 200", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "Future Secret") {
		t.Fatal("future post leaked in search page")
	}
}

func TestRestoreRevisionMustBelongToRouteContent(t *testing.T) {
	app, secret, adminID := newSecurityTestApp(t)
	ctx := context.Background()
	contributorID, err := app.Users.Save(ctx, services.SaveUserInput{Name: "contrib2", Password: "secret123", Mail: "c2@example.com", Role: "contributor"}, 0)
	if err != nil {
		t.Fatal(err)
	}
	ownID, err := app.Contents.Create(ctx, services.SaveContentInput{Title: "Own", Slug: "own", Text: "own", Type: models.ContentTypePost, Status: models.ContentStatusPost}, contributorID)
	if err != nil {
		t.Fatal(err)
	}
	otherID, err := app.Contents.Create(ctx, services.SaveContentInput{Title: "Other v1", Slug: "other", Text: "v1", Type: models.ContentTypePost, Status: models.ContentStatusPost}, adminID)
	if err != nil {
		t.Fatal(err)
	}
	if err := app.Contents.Update(ctx, otherID, services.SaveContentInput{Title: "Other v2", Slug: "other", Text: "v2", Type: models.ContentTypePost, Status: models.ContentStatusPost}); err != nil {
		t.Fatal(err)
	}
	revisions, err := app.Contents.Revisions(ctx, otherID)
	if err != nil || len(revisions) == 0 {
		t.Fatalf("expected other revision, got %v %#v", err, revisions)
	}

	form := url.Values{"_csrf": {adminToken(secret, contributorID)}, "rid": {itoa(revisions[0].RID)}}
	req := httptest.NewRequest(http.MethodPost, "/admin/posts/"+itoa(ownID)+"/restore", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	setSession(t, req, secret, contributorID)
	rec := httptest.NewRecorder()
	app.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("cross-content restore status = %d, want 403", rec.Code)
	}
	other, err := app.Contents.ByID(ctx, otherID)
	if err != nil {
		t.Fatal(err)
	}
	if other.Title != "Other v2" || other.Text != "v2" {
		t.Fatalf("other post changed after forbidden restore: %#v", other)
	}
}

func TestAutosaveAllowsOnlyPostOrPageAndChecksAuthor(t *testing.T) {
	app, secret, adminID := newSecurityTestApp(t)
	ctx := context.Background()
	contributorID, err := app.Users.Save(ctx, services.SaveUserInput{Name: "contrib3", Password: "secret123", Mail: "c3@example.com", Role: "contributor"}, 0)
	if err != nil {
		t.Fatal(err)
	}
	otherPostID, err := app.Contents.Create(ctx, services.SaveContentInput{Title: "Other", Slug: "other-auto", Text: "v1", Type: models.ContentTypePost, Status: models.ContentStatusPost}, adminID)
	if err != nil {
		t.Fatal(err)
	}

	form := url.Values{"_csrf": {adminToken(secret, contributorID)}, "type": {models.ContentTypeAttach}, "title": {"Bad"}, "status": {models.ContentStatusDraft}}
	req := httptest.NewRequest(http.MethodPost, "/admin/autosave", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	setSession(t, req, secret, contributorID)
	rec := httptest.NewRecorder()
	app.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("autosave attachment status = %d, want 400", rec.Code)
	}
	attachments, err := app.Contents.List(ctx, services.ContentQuery{Type: models.ContentTypeAttach, Status: "all", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(attachments) != 0 {
		t.Fatalf("autosave created attachment content: %#v", attachments)
	}

	form = url.Values{"_csrf": {adminToken(secret, contributorID)}, "type": {models.ContentTypePost}, "cid": {itoa(otherPostID)}, "title": {"Hijack"}, "status": {models.ContentStatusDraft}}
	req = httptest.NewRequest(http.MethodPost, "/admin/autosave", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	setSession(t, req, secret, contributorID)
	rec = httptest.NewRecorder()
	app.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("autosave another author's post status = %d, want 403", rec.Code)
	}
}

func TestPublicCommentWhitelistAndStopWords(t *testing.T) {
	app, _, adminID := newSecurityTestApp(t)
	ctx := context.Background()
	if err := app.Options.Set(ctx, "comments_whitelist", "1"); err != nil {
		t.Fatal(err)
	}
	if err := app.Options.Set(ctx, "comments_post_interval_enable", "0"); err != nil {
		t.Fatal(err)
	}
	if err := app.Options.Set(ctx, "comments_stop_words", "buy now"); err != nil {
		t.Fatal(err)
	}
	postID := createPublishedPost(t, app, adminID, "comment-white")
	if err := app.Comments.Save(ctx, services.SaveCommentInput{CID: postID, Author: "Known", Mail: "known@example.com", Text: "old", Status: "approved"}, 0); err != nil {
		t.Fatal(err)
	}

	rec := submitPublicComment(t, app, postID, url.Values{
		"author": {"Known"},
		"mail":   {"known@example.com"},
		"text":   {"normal comment"},
	}, "198.51.100.10")
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("known commenter status = %d, want 303", rec.Code)
	}

	rec = submitPublicComment(t, app, postID, url.Values{
		"author": {"New"},
		"mail":   {"new@example.com"},
		"text":   {"normal comment"},
	}, "198.51.100.11")
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("new commenter status = %d, want 303", rec.Code)
	}

	rec = submitPublicComment(t, app, postID, url.Values{
		"author": {"Spammer"},
		"mail":   {"spam@example.com"},
		"text":   {"please buy now"},
	}, "198.51.100.12")
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("spam commenter status = %d, want 303", rec.Code)
	}

	all, err := app.Comments.List(ctx, "all", "", postID)
	if err != nil {
		t.Fatal(err)
	}
	statuses := map[string]int{}
	for _, comment := range all {
		statuses[comment.Status]++
	}
	if statuses["approved"] != 2 || statuses["waiting"] != 1 || statuses["spam"] != 1 {
		t.Fatalf("comment statuses = %#v, want approved=2 waiting=1 spam=1", statuses)
	}
}

func TestPublicCommentRefererCheckRequiresCurrentContent(t *testing.T) {
	app, _, adminID := newSecurityTestApp(t)
	ctx := context.Background()
	if err := app.Options.Set(ctx, "comments_post_interval_enable", "0"); err != nil {
		t.Fatal(err)
	}
	postID := createPublishedPost(t, app, adminID, "comment-referer")
	form := url.Values{
		"author": {"Ref"},
		"mail":   {"ref@example.com"},
		"text":   {"referer check"},
	}

	rec := submitPublicCommentWithReferer(t, app, postID, form, "198.51.100.40", "")
	if rec.Code != http.StatusSeeOther || !strings.Contains(rec.Header().Get("Location"), "comment_error=referer") {
		t.Fatalf("empty referer response = %d %q", rec.Code, rec.Header().Get("Location"))
	}

	rec = submitPublicCommentWithReferer(t, app, postID, form, "198.51.100.41", "http://evil.example/post/comment-referer")
	if rec.Code != http.StatusSeeOther || !strings.Contains(rec.Header().Get("Location"), "comment_error=referer") {
		t.Fatalf("wrong host referer response = %d %q", rec.Code, rec.Header().Get("Location"))
	}

	rec = submitPublicCommentWithReferer(t, app, postID, form, "198.51.100.42", "http://example.com/post/other")
	if rec.Code != http.StatusSeeOther || !strings.Contains(rec.Header().Get("Location"), "comment_error=referer") {
		t.Fatalf("wrong path referer response = %d %q", rec.Code, rec.Header().Get("Location"))
	}

	rec = submitPublicCommentWithReferer(t, app, postID, form, "198.51.100.43", "http://example.com/post/comment-referer?comments_page=1")
	if rec.Code != http.StatusSeeOther || !strings.Contains(rec.Header().Get("Location"), "comment_ok=1") {
		t.Fatalf("valid referer response = %d %q", rec.Code, rec.Header().Get("Location"))
	}
}

func TestPublicCommentIPBlacklistAndParentValidation(t *testing.T) {
	app, _, adminID := newSecurityTestApp(t)
	ctx := context.Background()
	if err := app.Options.Set(ctx, "comments_ip_blacklist", "203.0.113.*"); err != nil {
		t.Fatal(err)
	}
	if err := app.Options.Set(ctx, "comments_post_interval_enable", "0"); err != nil {
		t.Fatal(err)
	}
	firstID := createPublishedPost(t, app, adminID, "first-parent")
	secondID := createPublishedPost(t, app, adminID, "second-parent")
	if err := app.Comments.Save(ctx, services.SaveCommentInput{CID: firstID, Author: "Parent", Mail: "p@example.com", Text: "parent", Status: "approved"}, 0); err != nil {
		t.Fatal(err)
	}
	parentComments, err := app.Comments.List(ctx, "approved", "", firstID)
	if err != nil || len(parentComments) == 0 {
		t.Fatalf("parent comment missing: %v %#v", err, parentComments)
	}

	rec := submitPublicComment(t, app, secondID, url.Values{
		"author": {"Blocked"},
		"mail":   {"b@example.com"},
		"text":   {"blocked"},
	}, "203.0.113.9")
	if rec.Code != http.StatusSeeOther || !strings.Contains(rec.Header().Get("Location"), "comment_error=blocked") {
		t.Fatalf("blacklist response = %d %q", rec.Code, rec.Header().Get("Location"))
	}

	rec = submitPublicComment(t, app, secondID, url.Values{
		"author": {"Child"},
		"mail":   {"c@example.com"},
		"text":   {"cross parent"},
		"parent": {itoa(parentComments[0].COID)},
	}, "198.51.100.20")
	if rec.Code != http.StatusSeeOther || !strings.Contains(rec.Header().Get("Location"), "comment_error=parent") {
		t.Fatalf("cross-parent response = %d %q", rec.Code, rec.Header().Get("Location"))
	}

	secondComments, err := app.Comments.List(ctx, "all", "", secondID)
	if err != nil {
		t.Fatal(err)
	}
	if len(secondComments) != 0 {
		t.Fatalf("unexpected second post comments: %#v", secondComments)
	}
}

func TestCommentPaginationKeepsThreadReplies(t *testing.T) {
	app, _, adminID := newSecurityTestApp(t)
	ctx := context.Background()
	if err := app.Options.Set(ctx, "comments_page_size", "1"); err != nil {
		t.Fatal(err)
	}
	if err := app.Options.Set(ctx, "comments_page_display", "first"); err != nil {
		t.Fatal(err)
	}
	postID := createPublishedPost(t, app, adminID, "comment-thread-page")
	if err := app.Comments.Save(ctx, services.SaveCommentInput{CID: postID, Author: "Root One", Mail: "r1@example.com", Text: "root one", Status: "approved"}, 0); err != nil {
		t.Fatal(err)
	}
	roots, err := app.Comments.List(ctx, "approved", "", postID)
	if err != nil || len(roots) == 0 {
		t.Fatalf("root comment missing: %v %#v", err, roots)
	}
	rootOneID := roots[len(roots)-1].COID
	if err := app.Comments.Save(ctx, services.SaveCommentInput{CID: postID, Author: "Reply", Mail: "reply@example.com", Text: "reply", Status: "approved", Parent: rootOneID}, 0); err != nil {
		t.Fatal(err)
	}
	if err := app.Comments.Save(ctx, services.SaveCommentInput{CID: postID, Author: "Root Two", Mail: "r2@example.com", Text: "root two", Status: "approved"}, 0); err != nil {
		t.Fatal(err)
	}
	post, err := app.Contents.ByID(ctx, postID)
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/post/comment-thread-page?comments_page=1", nil)
	views, pager, err := app.commentsForPost(req, post)
	if err != nil {
		t.Fatal(err)
	}
	if pager.Total != 2 || pager.TotalPages != 2 {
		t.Fatalf("pager = %#v, want 2 top-level threads over 2 pages", pager)
	}
	if len(views) != 2 || views[0].Author != "Root One" || views[1].Author != "Reply" || views[1].Parent != rootOneID {
		t.Fatalf("page 1 views = %#v, want root one with reply", views)
	}

	req = httptest.NewRequest(http.MethodGet, "/post/comment-thread-page?comments_page=2", nil)
	views, _, err = app.commentsForPost(req, post)
	if err != nil {
		t.Fatal(err)
	}
	if len(views) != 1 || views[0].Author != "Root Two" {
		t.Fatalf("page 2 views = %#v, want only root two", views)
	}
}

func TestPublicCommentNestingDepth(t *testing.T) {
	app, _, adminID := newSecurityTestApp(t)
	ctx := context.Background()
	if err := app.Options.Set(ctx, "comments_max_nesting_levels", "1"); err != nil {
		t.Fatal(err)
	}
	if err := app.Options.Set(ctx, "comments_post_interval_enable", "0"); err != nil {
		t.Fatal(err)
	}
	postID := createPublishedPost(t, app, adminID, "comment-depth")
	if err := app.Comments.Save(ctx, services.SaveCommentInput{CID: postID, Author: "Parent", Mail: "p@example.com", Text: "parent", Status: "approved"}, 0); err != nil {
		t.Fatal(err)
	}
	parentComments, err := app.Comments.List(ctx, "approved", "", postID)
	if err != nil || len(parentComments) == 0 {
		t.Fatalf("parent comment missing: %v %#v", err, parentComments)
	}

	rec := submitPublicComment(t, app, postID, url.Values{
		"author": {"Child"},
		"mail":   {"c@example.com"},
		"text":   {"too deep"},
		"parent": {itoa(parentComments[0].COID)},
	}, "198.51.100.30")
	if rec.Code != http.StatusSeeOther || !strings.Contains(rec.Header().Get("Location"), "comment_error=depth") {
		t.Fatalf("depth response = %d %q", rec.Code, rec.Header().Get("Location"))
	}
}

func TestAdminCommentBatchAndClearSpam(t *testing.T) {
	app, secret, adminID := newSecurityTestApp(t)
	ctx := context.Background()
	postID := createPublishedPost(t, app, adminID, "comment-batch")
	for _, input := range []services.SaveCommentInput{
		{CID: postID, Author: "One", Mail: "one@example.com", Text: "one", Status: "waiting"},
		{CID: postID, Author: "Two", Mail: "two@example.com", Text: "two", Status: "waiting"},
		{CID: postID, Author: "Spam", Mail: "spam@example.com", Text: "spam", Status: "spam"},
	} {
		if err := app.Comments.Save(ctx, input, 0); err != nil {
			t.Fatal(err)
		}
	}
	all, err := app.Comments.List(ctx, "all", "", postID)
	if err != nil {
		t.Fatal(err)
	}
	form := url.Values{"_csrf": {adminToken(secret, adminID)}, "action": {"approved"}}
	for _, comment := range all {
		if comment.Status == "waiting" {
			form.Add("id", itoa(comment.COID))
		}
	}
	req := httptest.NewRequest(http.MethodPost, "/admin/comments/batch", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	setSession(t, req, secret, adminID)
	rec := httptest.NewRecorder()
	app.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("batch approve status = %d, want 303", rec.Code)
	}
	approved, err := app.Comments.List(ctx, "approved", "", postID)
	if err != nil {
		t.Fatal(err)
	}
	if len(approved) != 2 {
		t.Fatalf("approved comments = %d, want 2", len(approved))
	}

	form = url.Values{"_csrf": {adminToken(secret, adminID)}, "action": {"spam"}}
	for _, comment := range approved {
		form.Add("id", itoa(comment.COID))
	}
	req = httptest.NewRequest(http.MethodPost, "/admin/comments/batch", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	setSession(t, req, secret, adminID)
	rec = httptest.NewRecorder()
	app.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("batch spam status = %d, want 303", rec.Code)
	}
	spam, err := app.Comments.List(ctx, "spam", "", postID)
	if err != nil {
		t.Fatal(err)
	}
	if len(spam) != 3 {
		t.Fatalf("spam comments = %d, want 3", len(spam))
	}

	form = url.Values{"_csrf": {adminToken(secret, adminID)}, "action": {"delete"}}
	form.Add("id", itoa(spam[0].COID))
	req = httptest.NewRequest(http.MethodPost, "/admin/comments/batch", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	setSession(t, req, secret, adminID)
	rec = httptest.NewRecorder()
	app.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("batch delete status = %d, want 303", rec.Code)
	}
	spam, err = app.Comments.List(ctx, "spam", "", postID)
	if err != nil {
		t.Fatal(err)
	}
	if len(spam) != 2 {
		t.Fatalf("spam comments after delete = %d, want 2", len(spam))
	}

	form = url.Values{"_csrf": {adminToken(secret, adminID)}}
	req = httptest.NewRequest(http.MethodPost, "/admin/comments/clear-spam", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	setSession(t, req, secret, adminID)
	rec = httptest.NewRecorder()
	app.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("clear spam status = %d, want 303", rec.Code)
	}
	spam, err = app.Comments.List(ctx, "spam", "", postID)
	if err != nil {
		t.Fatal(err)
	}
	if len(spam) != 0 {
		t.Fatalf("spam comments remain: %#v", spam)
	}
}

func TestCommentHTMLAllowListSanitizesTags(t *testing.T) {
	got := string(sanitizeCommentHTML(`<strong>ok</strong><script>alert(1)</script><a href="example.com" onclick="x">site</a>`, "strong,a", true))
	if !strings.Contains(got, "<strong>ok</strong>") {
		t.Fatalf("strong tag not preserved: %s", got)
	}
	if strings.Contains(got, "<script>") || strings.Contains(got, "onclick") {
		t.Fatalf("unsafe html preserved: %s", got)
	}
	if !strings.Contains(got, `<a href="https://example.com" rel="nofollow">site</a>`) {
		t.Fatalf("safe link not normalized: %s", got)
	}
	for _, raw := range []string{`<a href="javascript:alert(1)">x</a>`, `<a href="data:text/html,x">x</a>`} {
		got = string(sanitizeCommentHTML(raw, "a", true))
		if strings.Contains(got, "javascript:") || strings.Contains(got, "data:text") || strings.Contains(got, "href=") {
			t.Fatalf("dangerous href preserved for %q: %s", raw, got)
		}
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

func createPublishedPost(t *testing.T, app *App, authorID int64, slug string) int64 {
	t.Helper()
	id, err := app.Contents.Create(context.Background(), services.SaveContentInput{
		Title:        slug,
		Slug:         slug,
		Text:         "body",
		Type:         models.ContentTypePost,
		Status:       models.ContentStatusPost,
		AllowComment: true,
	}, authorID)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func submitPublicComment(t *testing.T, app *App, cid int64, form url.Values, ip string) *httptest.ResponseRecorder {
	t.Helper()
	post, err := app.Contents.ByID(context.Background(), cid)
	if err != nil {
		t.Fatal(err)
	}
	return submitPublicCommentWithReferer(t, app, cid, form, ip, "http://example.com"+contentPublicURL(post))
}

func submitPublicCommentWithReferer(t *testing.T, app *App, cid int64, form url.Values, ip, referer string) *httptest.ResponseRecorder {
	t.Helper()
	form = cloneValues(form)
	form.Set("cid", itoa(cid))
	req := httptest.NewRequest(http.MethodPost, "/comment", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-Real-IP", ip)
	form.Set("_csrf", app.csrfTokenFor(req, "comment"))
	req = httptest.NewRequest(http.MethodPost, "/comment", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-Real-IP", ip)
	if referer != "" {
		req.Header.Set("Referer", referer)
	}
	rec := httptest.NewRecorder()
	app.Handler().ServeHTTP(rec, req)
	return rec
}

func cloneValues(values url.Values) url.Values {
	out := url.Values{}
	for key, items := range values {
		for _, item := range items {
			out.Add(key, item)
		}
	}
	return out
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
