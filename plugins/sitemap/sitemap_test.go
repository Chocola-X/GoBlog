package sitemap

import (
	"context"
	"encoding/xml"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"goblog/core/plugin"
)

func TestSitemapUsesPublicPostURLs(t *testing.T) {
	created := time.Date(2026, 7, 15, 10, 30, 0, 0, time.UTC).Unix()
	rt := &plugin.Runtime{
		ListPublished: func(context.Context, int, int) ([]plugin.PublicContent, error) {
			return []plugin.PublicContent{
				{CID: 10, SlugID: 1, Created: created},
				{CID: 11, Slug: "custom-slug", SlugID: 2, Created: created, Modified: created + 3600},
			}, nil
		},
		Option: func(context.Context, string) (string, error) {
			return "http://localhost:8080/", nil
		},
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/sitemap.xml", nil)

	handleSitemap(rt, rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var doc urlSet
	if err := xml.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
		t.Fatalf("invalid sitemap xml: %v\n%s", err, rec.Body.String())
	}
	got := make([]string, 0, len(doc.URLs))
	for _, item := range doc.URLs {
		got = append(got, item.Loc)
	}
	joined := strings.Join(got, "\n")
	for _, want := range []string{
		"http://localhost:8080/",
		"http://localhost:8080/post/1.html",
		"http://localhost:8080/post/custom-slug.html",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("sitemap URLs = %v, missing %q", got, want)
		}
	}
	if strings.Contains(joined, "http://localhost:8080/post/\n") {
		t.Fatalf("sitemap contains empty post URL: %v", got)
	}
	for _, item := range doc.URLs {
		if item.LastMod == "" {
			t.Fatalf("lastmod missing for %#v", item)
		}
	}
}

func TestSitemapBaseURLFallsBackToRequestHost(t *testing.T) {
	rt := &plugin.Runtime{
		ListPublished: func(context.Context, int, int) ([]plugin.PublicContent, error) {
			return []plugin.PublicContent{{CID: 7, Created: time.Now().Unix()}}, nil
		},
		Option: func(context.Context, string) (string, error) {
			return "", nil
		},
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "http://example.test/sitemap.xml", nil)

	handleSitemap(rt, rec, req)

	if !strings.Contains(rec.Body.String(), "<loc>http://example.test/post/7.html</loc>") {
		t.Fatalf("sitemap did not use request host fallback:\n%s", rec.Body.String())
	}
}
