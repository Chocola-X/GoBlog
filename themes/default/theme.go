package defaulttheme

import (
	"embed"
	"io/fs"

	"goblog/core/plugin"
)

//go:embed templates/* static/*
var themeFS embed.FS

func init() {
	static, _ := fs.Sub(themeFS, "static")
	plugin.RegisterTheme(plugin.Theme{
		Name:        "default",
		Description: "Typecho 默认主题启发的极简 MDUI 2 主题",
		Templates:   themeFS,
		Static:      static,
	})
}
