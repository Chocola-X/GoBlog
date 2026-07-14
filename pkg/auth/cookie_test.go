package auth

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestVersionedSessionRoundTripAndTamper(t *testing.T) {
	recorder := httptest.NewRecorder()
	SetVersionedSessionWithOptions(recorder, "secret", 42, "revision-2", CookieOptions{})
	cookies := recorder.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("cookies = %d, want 1", len(cookies))
	}
	req := httptest.NewRequest("GET", "/", nil)
	req.AddCookie(cookies[0])
	session, ok := ParseVersionedSessionWithOptions(req, "secret", CookieOptions{})
	if !ok || session.UID != 42 || session.Version != "revision-2" {
		t.Fatalf("parsed session = %#v, %v", session, ok)
	}

	tampered := *cookies[0]
	tampered.Value = strings.Replace(tampered.Value, "42:", "43:", 1)
	req = httptest.NewRequest("GET", "/", nil)
	req.AddCookie(&tampered)
	if _, ok := ParseVersionedSessionWithOptions(req, "secret", CookieOptions{}); ok {
		t.Fatal("tampered session was accepted")
	}
}

func TestLegacySessionCookieStillParses(t *testing.T) {
	recorder := httptest.NewRecorder()
	SetSession(recorder, "secret", 7)
	cookie := recorder.Result().Cookies()[0]
	parts := strings.Split(cookie.Value, ":")
	if len(parts) != 4 {
		t.Fatalf("new compatibility cookie parts = %d, want 4", len(parts))
	}
	payload := parts[0] + ":" + parts[1]
	cookie.Value = payload + ":" + sign("secret", payload)
	req := httptest.NewRequest("GET", "/", nil)
	req.AddCookie(cookie)
	session, ok := ParseVersionedSessionWithOptions(req, "secret", CookieOptions{})
	if !ok || session.UID != 7 || session.Version != "" {
		t.Fatalf("legacy session = %#v, %v", session, ok)
	}
}
