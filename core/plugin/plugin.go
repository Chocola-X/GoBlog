package plugin

import (
	"context"
	"html/template"
	"io/fs"
	"net/http"
	"sync"
)

type PublicContent struct {
	CID      int64
	Title    string
	Slug     string
	Created  int64
	Modified int64
	Text     string
	Type     string
	Status   string
}

type Runtime struct {
	ListPublished func(context.Context, int, int) ([]PublicContent, error)
	Option        func(context.Context, string) (string, error)
}

type RouteHandler func(*Runtime, http.ResponseWriter, *http.Request)

type Route struct {
	Method  string
	Pattern string
	Handler RouteHandler
}

type HookFunc func(context.Context, any) (any, error)

type Plugin interface {
	Name() string
	Version() string
	Description() string
	Init(*Manager)
}

type Theme struct {
	Name        string
	Description string
	Templates   fs.FS
	Static      fs.FS
	Funcs       template.FuncMap
}

type Manager struct {
	mu      sync.RWMutex
	plugins []Plugin
	hooks   map[string][]HookFunc
	routes  []Route
	themes  map[string]Theme
}

var Default = NewManager()

func NewManager() *Manager {
	return &Manager{
		hooks:  make(map[string][]HookFunc),
		themes: make(map[string]Theme),
	}
}

func Register(p Plugin) {
	Default.Register(p)
}

func RegisterTheme(theme Theme) {
	Default.RegisterTheme(theme)
}

func (m *Manager) Register(p Plugin) {
	m.mu.Lock()
	m.plugins = append(m.plugins, p)
	m.mu.Unlock()
	p.Init(m)
}

func (m *Manager) RegisterHook(name string, fn HookFunc) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.hooks[name] = append(m.hooks[name], fn)
}

func (m *Manager) Apply(ctx context.Context, name string, payload any) (any, error) {
	m.mu.RLock()
	hooks := append([]HookFunc(nil), m.hooks[name]...)
	m.mu.RUnlock()

	var err error
	for _, hook := range hooks {
		payload, err = hook(ctx, payload)
		if err != nil {
			return nil, err
		}
	}

	return payload, nil
}

func (m *Manager) RegisterRoute(method, pattern string, handler RouteHandler) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.routes = append(m.routes, Route{Method: method, Pattern: pattern, Handler: handler})
}

func (m *Manager) Routes() []Route {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return append([]Route(nil), m.routes...)
}

func (m *Manager) RegisterTheme(theme Theme) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.themes[theme.Name] = theme
}

func (m *Manager) Theme(name string) (Theme, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	theme, ok := m.themes[name]
	return theme, ok
}

func (m *Manager) Plugins() []Plugin {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return append([]Plugin(nil), m.plugins...)
}
