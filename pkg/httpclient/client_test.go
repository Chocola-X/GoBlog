package httpclient

import (
	"net/http"
	"testing"
	"time"
)

func TestRedirectToPrivateAddressIsRejected(t *testing.T) {
	client, err := New(Config{Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodGet, "http://127.0.0.1/admin", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := client.httpClient.CheckRedirect(req, nil); err == nil {
		t.Fatal("expected redirect to private address to be rejected")
	}
}

func TestRedirectLimit(t *testing.T) {
	client, err := New(Config{Timeout: time.Second, AllowPrivate: true})
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodGet, "http://example.com", nil)
	if err != nil {
		t.Fatal(err)
	}
	via := []*http.Request{req, req, req, req, req}
	if err := client.httpClient.CheckRedirect(req, via); err == nil {
		t.Fatal("expected redirect limit error")
	}
}
