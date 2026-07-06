package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const CookieName = "goblog_session"

type CookieOptions struct {
	Prefix   string
	Secure   bool
	HTTPOnly bool
	SameSite http.SameSite
}

func (o CookieOptions) Name(base string) string {
	if o.Prefix == "" {
		return base
	}
	return o.Prefix + base
}

func SetSession(w http.ResponseWriter, secret string, uid int64) {
	SetSessionWithOptions(w, secret, uid, CookieOptions{})
}

func SetSessionWithOptions(w http.ResponseWriter, secret string, uid int64, options CookieOptions) {
	if !options.HTTPOnly {
		options.HTTPOnly = true
	}
	if options.SameSite == 0 {
		options.SameSite = http.SameSiteLaxMode
	}
	exp := time.Now().Add(7 * 24 * time.Hour).Unix()
	payload := fmt.Sprintf("%d:%d", uid, exp)
	sig := sign(secret, payload)
	http.SetCookie(w, &http.Cookie{
		Name:     options.Name(CookieName),
		Value:    payload + ":" + sig,
		Path:     "/",
		HttpOnly: options.HTTPOnly,
		SameSite: options.SameSite,
		Secure:   options.Secure,
		Expires:  time.Unix(exp, 0),
	})
}

func ClearSession(w http.ResponseWriter) {
	ClearSessionWithOptions(w, CookieOptions{})
}

func ClearSessionWithOptions(w http.ResponseWriter, options CookieOptions) {
	if !options.HTTPOnly {
		options.HTTPOnly = true
	}
	if options.SameSite == 0 {
		options.SameSite = http.SameSiteLaxMode
	}
	http.SetCookie(w, &http.Cookie{
		Name:     options.Name(CookieName),
		Value:    "",
		Path:     "/",
		HttpOnly: options.HTTPOnly,
		SameSite: options.SameSite,
		Secure:   options.Secure,
		MaxAge:   -1,
	})
}

func ParseSession(r *http.Request, secret string) (int64, bool) {
	return ParseSessionWithOptions(r, secret, CookieOptions{})
}

func ParseSessionWithOptions(r *http.Request, secret string, options CookieOptions) (int64, bool) {
	cookie, err := r.Cookie(options.Name(CookieName))
	if err != nil {
		return 0, false
	}

	parts := strings.Split(cookie.Value, ":")
	if len(parts) != 3 {
		return 0, false
	}

	payload := parts[0] + ":" + parts[1]
	if !hmac.Equal([]byte(sign(secret, payload)), []byte(parts[2])) {
		return 0, false
	}

	exp, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil || time.Now().Unix() > exp {
		return 0, false
	}

	uid, err := strconv.ParseInt(parts[0], 10, 64)
	return uid, err == nil
}

func sign(secret, payload string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
