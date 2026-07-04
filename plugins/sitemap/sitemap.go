package sitemap

import (
	"encoding/xml"
	"net/http"
	"strings"
	"time"

	"goblog/core/plugin"
)

type sitemapPlugin struct{}

func init() {
	plugin.Register(sitemapPlugin{})
}

func (sitemapPlugin) Name() string {
	return "sitemap"
}

func (sitemapPlugin) Version() string {
	return "0.1.0"
}

func (sitemapPlugin) Description() string {
	return "生成 /sitemap.xml，展示已发布文章。"
}

func (sitemapPlugin) Init(m *plugin.Manager) {
	m.RegisterRoute(http.MethodGet, "/sitemap.xml", handleSitemap)
}

func handleSitemap(rt *plugin.Runtime, w http.ResponseWriter, r *http.Request) {
	posts, err := rt.ListPublished(r.Context(), 1000, 0)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	baseURL, _ := rt.Option(r.Context(), "base_url")
	baseURL = strings.TrimRight(baseURL, "/")

	doc := urlSet{XMLNS: "http://www.sitemaps.org/schemas/sitemap/0.9"}
	doc.URLs = append(doc.URLs, urlEntry{Loc: baseURL + "/", LastMod: time.Now().Format("2006-01-02")})
	for _, post := range posts {
		lastMod := post.Modified
		if lastMod == 0 {
			lastMod = post.Created
		}
		doc.URLs = append(doc.URLs, urlEntry{
			Loc:     baseURL + "/post/" + post.Slug,
			LastMod: time.Unix(lastMod, 0).Format("2006-01-02"),
		})
	}

	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	_, _ = w.Write([]byte(xml.Header))
	_ = xml.NewEncoder(w).Encode(doc)
}

type urlSet struct {
	XMLName xml.Name   `xml:"urlset"`
	XMLNS   string     `xml:"xmlns,attr"`
	URLs    []urlEntry `xml:"url"`
}

type urlEntry struct {
	Loc     string `xml:"loc"`
	LastMod string `xml:"lastmod,omitempty"`
}
