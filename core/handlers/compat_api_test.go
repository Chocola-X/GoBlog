package handlers

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"goblog/core/models"
	"goblog/core/services"
)

func TestXMLRPCAuthAndPermission(t *testing.T) {
	app, _, adminID := newSecurityTestApp(t)
	ctx := context.Background()
	if _, err := app.Users.Save(ctx, services.SaveUserInput{Name: "subxml", Password: "secret123", Mail: "subxml@example.com", Role: "subscriber"}, 0); err != nil {
		t.Fatal(err)
	}
	body := xmlRPCRequest("metaWeblog.newPost",
		xmlValueString("1"),
		xmlValueString("admin"),
		xmlValueString("wrong"),
		xmlValueStruct(map[string]string{"title": "Bad", "description": "body"}),
		"<boolean>1</boolean>",
	)
	rec := httptest.NewRecorder()
	app.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/xmlrpc.php", strings.NewReader(body)))
	if !strings.Contains(rec.Body.String(), "authentication failed") {
		t.Fatalf("expected authentication fault, got %s", rec.Body.String())
	}

	body = xmlRPCRequest("metaWeblog.newPost",
		xmlValueString("1"),
		xmlValueString("subxml"),
		xmlValueString("secret123"),
		xmlValueStruct(map[string]string{"title": "Denied", "description": "body"}),
		"<boolean>1</boolean>",
	)
	rec = httptest.NewRecorder()
	app.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/xmlrpc.php", strings.NewReader(body)))
	if !strings.Contains(rec.Body.String(), "permission denied") {
		t.Fatalf("expected permission fault, got %s", rec.Body.String())
	}

	body = xmlRPCRequest("metaWeblog.newPost",
		xmlValueString("1"),
		xmlValueString("admin"),
		xmlValueString("admin123"),
		xmlValueStruct(map[string]string{"title": "Created via XML-RPC", "description": "body"}),
		"<boolean>1</boolean>",
	)
	rec = httptest.NewRecorder()
	app.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/xmlrpc.php", strings.NewReader(body)))
	if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), "<fault>") {
		t.Fatalf("expected XML-RPC success, status=%d body=%s", rec.Code, rec.Body.String())
	}
	items, err := app.Contents.List(ctx, services.ContentQuery{Type: models.ContentTypePost, Status: "all", AuthorID: adminID, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) == 0 || items[0].Title != "Created via XML-RPC" {
		t.Fatalf("xml-rpc post not saved: %#v", items)
	}
}

func TestPingbackRejectsSourceWithoutTarget(t *testing.T) {
	app, _, adminID := newSecurityTestApp(t)
	app.HTTPFetch = func(context.Context, string) (string, error) {
		return "<html>no target here</html>", nil
	}
	ctx := context.Background()
	postID, err := app.Contents.Create(ctx, services.SaveContentInput{Title: "Ping Target", Slug: "ping-target", Text: "body", Type: models.ContentTypePost, Status: models.ContentStatusPost, AllowPing: true, AllowComment: true}, adminID)
	if err != nil {
		t.Fatal(err)
	}
	post, _ := app.Contents.ByID(ctx, postID)
	target := "http://example.test" + app.contentURL(ctx, post)
	body := xmlRPCRequest("pingback.ping", xmlValueString("https://source.example/post"), xmlValueString(target))
	rec := httptest.NewRecorder()
	app.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/xmlrpc.php", strings.NewReader(body)))
	if !strings.Contains(rec.Body.String(), "source does not link to target") {
		t.Fatalf("expected missing target fault, got %s", rec.Body.String())
	}
}

func TestPingbackRejectsSameSiteSource(t *testing.T) {
	app, _, adminID := newSecurityTestApp(t)
	ctx := context.Background()
	if err := app.Options.Set(ctx, "base_url", "http://example.test"); err != nil {
		t.Fatal(err)
	}
	postID, err := app.Contents.Create(ctx, services.SaveContentInput{Title: "Ping Target", Slug: "same-site-target", Text: "body", Type: models.ContentTypePost, Status: models.ContentStatusPost, AllowPing: true, AllowComment: true}, adminID)
	if err != nil {
		t.Fatal(err)
	}
	post, _ := app.Contents.ByID(ctx, postID)
	body := xmlRPCRequest("pingback.ping", xmlValueString("http://example.test/post/source"), xmlValueString("http://example.test"+app.contentURL(ctx, post)))
	rec := httptest.NewRecorder()
	app.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/xmlrpc.php", strings.NewReader(body)))
	if !strings.Contains(rec.Body.String(), "self pingback is not allowed") {
		t.Fatalf("expected self pingback fault, got %s", rec.Body.String())
	}
	comments, err := app.Comments.ListFiltered(ctx, "all", "", postID, "pingback")
	if err != nil {
		t.Fatal(err)
	}
	if len(comments) != 0 {
		t.Fatalf("self pingback saved %d comments", len(comments))
	}
}

func TestMovableTypeCategoriesAndPublish(t *testing.T) {
	app, _, adminID := newSecurityTestApp(t)
	ctx := context.Background()
	categoryID, err := app.Metas.Save(ctx, services.SaveMetaInput{Name: "MT Category", Slug: "mt-category", Type: "category"}, 0)
	if err != nil {
		t.Fatal(err)
	}
	postID, err := app.Contents.Create(ctx, services.SaveContentInput{Title: "MT Draft", Slug: "mt-draft", Text: "body", Type: models.ContentTypePost, Status: models.ContentStatusDraft, AllowComment: true, AllowFeed: true}, adminID)
	if err != nil {
		t.Fatal(err)
	}
	if _, fault := app.handleXMLRPCMethod(ctx, "mt.setPostCategories", []xmlRPCParam{
		xmlParamInt(postID),
		xmlParamString("admin"),
		xmlParamString("admin123"),
		{Value: xmlRPCValue{Array: []xmlRPCValue{{Struct: []xmlRPCMember{{Name: "categoryId", Value: xmlRPCValue{String: ptrString(strconv.FormatInt(categoryID, 10))}}, {Name: "categoryName", Value: xmlRPCValue{String: ptrString("MT Category")}}}}}}},
	}); fault != nil {
		t.Fatalf("setPostCategories fault: %#v", fault)
	}
	result, fault := app.handleXMLRPCMethod(ctx, "mt.getPostCategories", []xmlRPCParam{xmlParamInt(postID), xmlParamString("admin"), xmlParamString("admin123")})
	if fault != nil {
		t.Fatalf("getPostCategories fault: %#v", fault)
	}
	categories, ok := result.([]any)
	if !ok || len(categories) != 1 {
		t.Fatalf("categories = %#v", result)
	}
	if _, fault := app.handleXMLRPCMethod(ctx, "mt.publishPost", []xmlRPCParam{xmlParamInt(postID), xmlParamString("admin"), xmlParamString("admin123")}); fault != nil {
		t.Fatalf("publishPost fault: %#v", fault)
	}
	post, err := app.Contents.ByID(ctx, postID)
	if err != nil {
		t.Fatal(err)
	}
	if post.Status != models.ContentStatusPost {
		t.Fatalf("post status = %q", post.Status)
	}
}

func TestMovableTypeSetCategoriesPermission(t *testing.T) {
	app, _, adminID := newSecurityTestApp(t)
	ctx := context.Background()
	if _, err := app.Users.Save(ctx, services.SaveUserInput{Name: "mtcontrib", Password: "secret123", Mail: "mtc@example.com", Role: "contributor"}, 0); err != nil {
		t.Fatal(err)
	}
	postID, err := app.Contents.Create(ctx, services.SaveContentInput{Title: "Other Post", Slug: "mt-other", Text: "body", Type: models.ContentTypePost, Status: models.ContentStatusDraft}, adminID)
	if err != nil {
		t.Fatal(err)
	}
	_, fault := app.handleXMLRPCMethod(ctx, "mt.setPostCategories", []xmlRPCParam{
		xmlParamInt(postID),
		xmlParamString("mtcontrib"),
		xmlParamString("secret123"),
		{Value: xmlRPCValue{Array: []xmlRPCValue{}}},
	})
	if fault == nil || fault.Message != "permission denied" {
		t.Fatalf("expected permission denied fault, got %#v", fault)
	}
}

func TestTrackbackDedupe(t *testing.T) {
	app, _, adminID := newSecurityTestApp(t)
	ctx := context.Background()
	postID, err := app.Contents.Create(ctx, services.SaveContentInput{Title: "Track Target", Slug: "track-target", Text: "body", Type: models.ContentTypePost, Status: models.ContentStatusPost, AllowPing: true, AllowComment: true}, adminID)
	if err != nil {
		t.Fatal(err)
	}
	form := url.Values{"url": {"https://source.example/post"}, "title": {"Source"}, "excerpt": {"Excerpt"}, "blog_name": {"Source Blog"}}
	req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/trackback/%d", postID), strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	app.Handler().ServeHTTP(rec, req)
	if !strings.Contains(rec.Body.String(), "<error>0</error>") {
		t.Fatalf("first trackback failed: %s", rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodPost, fmt.Sprintf("/trackback/%d", postID), strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec = httptest.NewRecorder()
	app.Handler().ServeHTTP(rec, req)
	if !strings.Contains(rec.Body.String(), "duplicate trackback") {
		t.Fatalf("duplicate trackback not rejected: %s", rec.Body.String())
	}
	comments, err := app.Comments.ListFiltered(ctx, "all", "", postID, "trackback")
	if err != nil {
		t.Fatal(err)
	}
	if len(comments) != 1 {
		t.Fatalf("trackback count = %d, want 1", len(comments))
	}
}

func xmlRPCRequest(method string, values ...string) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0"?><methodCall><methodName>`)
	b.WriteString(method)
	b.WriteString(`</methodName><params>`)
	for _, value := range values {
		b.WriteString(`<param><value>`)
		b.WriteString(value)
		b.WriteString(`</value></param>`)
	}
	b.WriteString(`</params></methodCall>`)
	return b.String()
}

func xmlValueString(value string) string {
	return "<string>" + value + "</string>"
}

func xmlValueStruct(values map[string]string) string {
	var b strings.Builder
	b.WriteString("<struct>")
	for key, value := range values {
		b.WriteString("<member><name>")
		b.WriteString(key)
		b.WriteString("</name><value><string>")
		b.WriteString(value)
		b.WriteString("</string></value></member>")
	}
	b.WriteString("</struct>")
	return b.String()
}

func xmlParamString(value string) xmlRPCParam {
	return xmlRPCParam{Value: xmlRPCValue{String: ptrString(value)}}
}

func xmlParamInt(value int64) xmlRPCParam {
	return xmlRPCParam{Value: xmlRPCValue{String: ptrString(strconv.FormatInt(value, 10))}}
}

func ptrString(value string) *string {
	return &value
}
