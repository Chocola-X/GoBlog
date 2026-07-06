package validate

import (
	"net/mail"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

type Errors map[string][]string

func (e Errors) Add(field, message string) {
	if field == "" {
		field = "_"
	}
	e[field] = append(e[field], message)
}

func (e Errors) Has(field string) bool {
	return len(e[field]) > 0
}

func (e Errors) First(field string) string {
	if len(e[field]) == 0 {
		return ""
	}
	return e[field][0]
}

func (e Errors) Empty() bool {
	return len(e) == 0
}

type Validator struct {
	Errors Errors
}

func New() *Validator {
	return &Validator{Errors: Errors{}}
}

func (v *Validator) Required(field, value string) *Validator {
	if strings.TrimSpace(value) == "" {
		v.Errors.Add(field, "不能为空")
	}
	return v
}

func (v *Validator) MinLength(field, value string, min int) *Validator {
	if strings.TrimSpace(value) != "" && utf8.RuneCountInString(value) < min {
		v.Errors.Add(field, "长度不能少于 "+strconv.Itoa(min)+" 个字符")
	}
	return v
}

func (v *Validator) MaxLength(field, value string, max int) *Validator {
	if utf8.RuneCountInString(value) > max {
		v.Errors.Add(field, "长度不能超过 "+strconv.Itoa(max)+" 个字符")
	}
	return v
}

func (v *Validator) Email(field, value string) *Validator {
	value = strings.TrimSpace(value)
	if value == "" {
		return v
	}
	if _, err := mail.ParseAddress(value); err != nil {
		v.Errors.Add(field, "邮箱格式不正确")
	}
	return v
}

func (v *Validator) URL(field, value string) *Validator {
	value = strings.TrimSpace(value)
	if value == "" {
		return v
	}
	u, err := url.ParseRequestURI(value)
	if err != nil || u.Scheme == "" || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		v.Errors.Add(field, "URL 格式不正确")
	}
	return v
}

func (v *Validator) Integer(field, value string) *Validator {
	value = strings.TrimSpace(value)
	if value == "" {
		return v
	}
	if _, err := strconv.ParseInt(value, 10, 64); err != nil {
		v.Errors.Add(field, "必须是整数")
	}
	return v
}

func (v *Validator) In(field, value string, allowed ...string) *Validator {
	for _, item := range allowed {
		if value == item {
			return v
		}
	}
	v.Errors.Add(field, "值不在允许范围内")
	return v
}

var slugPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]*$`)

func (v *Validator) Slug(field, value string) *Validator {
	value = strings.TrimSpace(value)
	if value == "" {
		return v
	}
	if !slugPattern.MatchString(value) {
		v.Errors.Add(field, "只能包含字母、数字、下划线和短横线，并以字母或数字开头")
	}
	return v
}

func (v *Validator) SafeText(field, value string) *Validator {
	if strings.ContainsAny(value, "\x00") {
		v.Errors.Add(field, "包含非法字符")
	}
	return v
}
