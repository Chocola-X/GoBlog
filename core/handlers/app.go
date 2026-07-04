package handlers

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"

	"goblog/admin"
	"goblog/core/models"
	"goblog/core/plugin"
	"goblog/core/services"
	"goblog/pkg/auth"
	"goblog/pkg/render"
)

type App struct {
	Contents *services.ContentService
	Users    *services.UserService
	Options  *services.OptionService
	Plugins  *plugin.Manager
}

func New(contents *services.ContentService, users *services.UserService, options *services.OptionService, plugins *plugin.Manager) *App {
	return &App{Contents: contents, Users: users, Options: options, Plugins: plugins}
}

func (a *App) Handler() http.Handler {
	mux := http.NewServeMux()

	adminAssets, _ := fs.Sub(admin.FS, "assets")
	mux.Handle("/admin/assets/", http.StripPrefix("/admin/assets/", http.FileServer(http.FS(adminAssets))))

	if theme, ok := a.activeTheme(context.Background()); ok && theme.Static != nil {
		mux.Handle("/theme/default/", http.StripPrefix("/theme/default/", http.FileServer(http.FS(theme.Static))))
	}

	mux.HandleFunc("/admin/login", a.adminLogin)
	mux.HandleFunc("/admin/logout", a.adminLogout)
	mux.HandleFunc("/admin", a.requireAdmin(a.adminDashboard))
	mux.HandleFunc("/admin/", a.requireAdmin(a.adminDashboard))
	mux.HandleFunc("/admin/posts", a.requireAdmin(a.adminPosts))
	mux.HandleFunc("/admin/posts/", a.requireAdmin(a.adminPostRoutes))
	mux.HandleFunc("/admin/options", a.requireAdmin(a.adminOptions))

	runtime := &plugin.Runtime{
		ListPublished: a.Contents.ListPublishedPlugin,
		Option:        a.Options.Get,
	}
	for _, route := range a.Plugins.Routes() {
		route := route
		mux.HandleFunc(route.Pattern, func(w http.ResponseWriter, r *http.Request) {
			if route.Method != "" && r.Method != route.Method {
				w.Header().Set("Allow", route.Method)
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			route.Handler(runtime, w, r)
		})
	}

	mux.HandleFunc("/post/", a.frontPost)
	mux.HandleFunc("/", a.frontIndex)

	return mux
}

func (a *App) adminLogin(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		a.renderAdmin(w, r, "login.html", map[string]any{"Title": "登录"})
	case http.MethodPost:
		if err := r.ParseForm(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		user, err := a.Users.Authenticate(r.Context(), r.FormValue("name"), r.FormValue("password"))
		if err != nil {
			a.renderAdmin(w, r, "login.html", map[string]any{"Title": "登录", "Error": "用户名或密码不正确"})
			return
		}
		secret, _ := a.Options.Get(r.Context(), "auth_secret")
		auth.SetSession(w, secret, user.UID)
		http.Redirect(w, r, "/admin", http.StatusSeeOther)
	default:
		methodNotAllowed(w, http.MethodGet+", "+http.MethodPost)
	}
}

func (a *App) adminLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return
	}
	auth.ClearSession(w)
	http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
}

func (a *App) adminDashboard(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/admin" && r.URL.Path != "/admin/" {
		http.NotFound(w, r)
		return
	}
	count, err := a.Contents.Count(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	plugins := a.Plugins.Plugins()
	a.renderAdmin(w, r, "dashboard.html", map[string]any{
		"Title":       "控制台",
		"PostCount":   count,
		"PluginCount": len(plugins),
		"Plugins":     plugins,
	})
}

func (a *App) adminPosts(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/admin/posts" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	posts, err := a.Contents.ListAll(r.Context(), 100, 0)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	a.renderAdmin(w, r, "posts.html", map[string]any{"Title": "文章", "Posts": posts})
}

func (a *App) adminPostRoutes(w http.ResponseWriter, r *http.Request) {
	clean := strings.Trim(strings.TrimPrefix(r.URL.Path, "/admin/posts/"), "/")
	if clean == "new" {
		a.adminPostNew(w, r)
		return
	}

	parts := strings.Split(clean, "/")
	if len(parts) < 2 {
		http.NotFound(w, r)
		return
	}
	id, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	switch parts[1] {
	case "edit":
		a.adminPostEdit(w, r, id)
	case "delete":
		a.adminPostDelete(w, r, id)
	default:
		http.NotFound(w, r)
	}
}

func (a *App) adminPostNew(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		a.renderAdmin(w, r, "post_form.html", map[string]any{
			"Title":  "写文章",
			"Post":   models.Content{Status: models.ContentStatusPost},
			"Action": "/admin/posts/new",
		})
	case http.MethodPost:
		input, err := parseContentForm(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		uid, _ := a.currentUserID(r)
		id, err := a.Contents.Create(r.Context(), input, uid)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		http.Redirect(w, r, fmt.Sprintf("/admin/posts/%d/edit?saved=1", id), http.StatusSeeOther)
	default:
		methodNotAllowed(w, http.MethodGet+", "+http.MethodPost)
	}
}

func (a *App) adminPostEdit(w http.ResponseWriter, r *http.Request, id int64) {
	post, err := a.Contents.ByID(r.Context(), id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	switch r.Method {
	case http.MethodGet:
		a.renderAdmin(w, r, "post_form.html", map[string]any{
			"Title":  "编辑文章",
			"Post":   post,
			"Action": fmt.Sprintf("/admin/posts/%d/edit", id),
			"Saved":  r.URL.Query().Get("saved") == "1",
		})
	case http.MethodPost:
		input, err := parseContentForm(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if err := a.Contents.Update(r.Context(), id, input); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		http.Redirect(w, r, fmt.Sprintf("/admin/posts/%d/edit?saved=1", id), http.StatusSeeOther)
	default:
		methodNotAllowed(w, http.MethodGet+", "+http.MethodPost)
	}
}

func (a *App) adminPostDelete(w http.ResponseWriter, r *http.Request, id int64) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return
	}
	if err := a.Contents.Delete(r.Context(), id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/admin/posts", http.StatusSeeOther)
}

func (a *App) adminOptions(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		options, err := a.Options.All(r.Context())
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		a.renderAdmin(w, r, "options.html", map[string]any{"Title": "设置", "Options": options, "Saved": r.URL.Query().Get("saved") == "1"})
	case http.MethodPost:
		if err := r.ParseForm(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		for _, key := range []string{"site_title", "site_description", "base_url", "active_theme"} {
			if err := a.Options.Set(r.Context(), key, r.FormValue(key)); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
		}
		http.Redirect(w, r, "/admin/options?saved=1", http.StatusSeeOther)
	default:
		methodNotAllowed(w, http.MethodGet+", "+http.MethodPost)
	}
}

func (a *App) frontIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	posts, err := a.Contents.ListPublished(r.Context(), 20, 0)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	a.renderTheme(w, r, "index.html", map[string]any{"Posts": posts})
}

func (a *App) frontPost(w http.ResponseWriter, r *http.Request) {
	postSlug := path.Base(strings.TrimSuffix(r.URL.Path, "/"))
	post, err := a.Contents.BySlug(r.Context(), postSlug)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			a.renderThemeStatus(w, r, "404.html", map[string]any{}, http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	a.renderTheme(w, r, "post.html", map[string]any{"Post": post, "ContentHTML": render.PlainTextHTML(post.Text)})
}

func (a *App) requireAdmin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, ok := a.currentUserID(r); !ok {
			http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
			return
		}
		next(w, r)
	}
}

func (a *App) currentUserID(r *http.Request) (int64, bool) {
	secret, err := a.Options.Get(r.Context(), "auth_secret")
	if err != nil || secret == "" {
		return 0, false
	}
	return auth.ParseSession(r, secret)
}

func (a *App) renderAdmin(w http.ResponseWriter, r *http.Request, page string, data map[string]any) {
	funcs := template.FuncMap{
		"date":        formatDate,
		"statusLabel": statusLabel,
		"excerpt":     render.Excerpt,
	}
	tmpl, err := template.New("base.html").Funcs(funcs).ParseFS(admin.FS, "templates/base.html", "templates/"+page)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	a.enrichData(r.Context(), data)
	if err := tmpl.ExecuteTemplate(w, "base", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (a *App) renderTheme(w http.ResponseWriter, r *http.Request, page string, data map[string]any) {
	a.renderThemeStatus(w, r, page, data, http.StatusOK)
}

func (a *App) renderThemeStatus(w http.ResponseWriter, r *http.Request, page string, data map[string]any, status int) {
	theme, ok := a.activeTheme(r.Context())
	if !ok {
		http.Error(w, "active theme not found", http.StatusInternalServerError)
		return
	}
	funcs := template.FuncMap{
		"date":    formatDate,
		"excerpt": render.Excerpt,
	}
	for name, fn := range theme.Funcs {
		funcs[name] = fn
	}
	tmpl, err := template.New("base.html").Funcs(funcs).ParseFS(theme.Templates, "templates/base.html", "templates/"+page)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	a.enrichData(r.Context(), data)
	w.WriteHeader(status)
	if err := tmpl.ExecuteTemplate(w, "base", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (a *App) activeTheme(ctx context.Context) (plugin.Theme, bool) {
	name, _ := a.Options.Get(ctx, "active_theme")
	if name == "" {
		name = "default"
	}
	return a.Plugins.Theme(name)
}

func (a *App) enrichData(ctx context.Context, data map[string]any) {
	options, err := a.Options.All(ctx)
	if err == nil {
		data["Site"] = options
	}
}

func parseContentForm(r *http.Request) (services.SaveContentInput, error) {
	if err := r.ParseForm(); err != nil {
		return services.SaveContentInput{}, err
	}
	return services.SaveContentInput{
		Title:  strings.TrimSpace(r.FormValue("title")),
		Slug:   strings.TrimSpace(r.FormValue("slug")),
		Text:   strings.TrimSpace(r.FormValue("text")),
		Status: r.FormValue("status"),
	}, nil
}

func formatDate(ts int64) string {
	if ts <= 0 {
		return ""
	}
	return time.Unix(ts, 0).Format("2006-01-02 15:04")
}

func statusLabel(status string) string {
	if status == models.ContentStatusDraft {
		return "草稿"
	}
	return "已发布"
}

func methodNotAllowed(w http.ResponseWriter, allow string) {
	w.Header().Set("Allow", allow)
	http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
}
