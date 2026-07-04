package render

import (
	"html"
	"html/template"
	"strings"
)

func PlainTextHTML(input string) template.HTML {
	escaped := html.EscapeString(input)
	escaped = strings.ReplaceAll(escaped, "\r\n", "\n")
	escaped = strings.ReplaceAll(escaped, "\n\n", "</p><p>")
	escaped = strings.ReplaceAll(escaped, "\n", "<br>")
	return template.HTML("<p>" + escaped + "</p>")
}

func Excerpt(input string, n int) string {
	text := strings.Join(strings.Fields(input), " ")
	if len([]rune(text)) <= n {
		return text
	}
	runes := []rune(text)
	return string(runes[:n]) + "..."
}
