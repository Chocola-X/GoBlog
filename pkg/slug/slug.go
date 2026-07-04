package slug

import (
	"strings"
	"unicode"
)

func Make(input string) string {
	input = strings.TrimSpace(strings.ToLower(input))
	var b strings.Builder
	dash := false

	for _, r := range input {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
			dash = false
		case r == '-' || r == '_' || unicode.IsSpace(r):
			if !dash && b.Len() > 0 {
				b.WriteByte('-')
				dash = true
			}
		}
	}

	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "post"
	}
	return out
}
