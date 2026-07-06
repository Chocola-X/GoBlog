package validate

import "testing"

func TestValidatorRules(t *testing.T) {
	v := New()
	v.Required("title", " ").
		MinLength("password", "abc", 6).
		MaxLength("name", "abcdef", 5).
		Email("mail", "bad-mail").
		URL("url", "javascript:alert(1)").
		Integer("sort", "x").
		In("role", "root", "administrator", "editor").
		Slug("slug", "../bad").
		SafeText("text", "ok\x00bad")

	for _, field := range []string{"title", "password", "name", "mail", "url", "sort", "role", "slug", "text"} {
		if !v.Errors.Has(field) {
			t.Fatalf("expected validation error for %s", field)
		}
	}
}

func TestValidatorAllowsOptionalEmptyValues(t *testing.T) {
	v := New()
	v.Email("mail", "").
		URL("url", "").
		Integer("sort", "").
		Slug("slug", "")

	if !v.Errors.Empty() {
		t.Fatalf("expected no errors for optional empty values, got %#v", v.Errors)
	}
}
