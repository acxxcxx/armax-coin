package main

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"testing"
)

func findCookie(cookies []*http.Cookie, name string) *http.Cookie {
	for _, c := range cookies {
		if c.Name == name {
			return c
		}
	}
	return nil
}

func TestCodeQL_G8_SessionCookiesContextAwareSecure(t *testing.T) {
	// 1. handleWebUI: loopback HTTP must keep Secure=false, while TLS must set Secure=true.
	reqHTTP := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:18088/", nil)
	recHTTP := httptest.NewRecorder()
	handleWebUI(recHTTP, reqHTTP)
	if recHTTP.Code != http.StatusOK {
		t.Fatalf("handleWebUI HTTP returned %d", recHTTP.Code)
	}
	cHTTP := findCookie(recHTTP.Result().Cookies(), "armax_gui_token")
	if cHTTP == nil || !cHTTP.HttpOnly || cHTTP.SameSite != http.SameSiteStrictMode || cHTTP.Secure {
		t.Fatalf("expected HttpOnly Strict non-Secure cookie on loopback HTTP, got %+v", cHTTP)
	}

	reqTLS := httptest.NewRequest(http.MethodGet, "https://127.0.0.1:18088/", nil)
	reqTLS.TLS = &tls.ConnectionState{}
	recTLS := httptest.NewRecorder()
	handleWebUI(recTLS, reqTLS)
	if recTLS.Code != http.StatusOK {
		t.Fatalf("handleWebUI HTTPS returned %d", recTLS.Code)
	}
	cTLS := findCookie(recTLS.Result().Cookies(), "armax_gui_token")
	if cTLS == nil || !cTLS.HttpOnly || cTLS.SameSite != http.SameSiteStrictMode || !cTLS.Secure {
		t.Fatalf("expected HttpOnly Strict Secure cookie on TLS request, got %+v", cTLS)
	}

	// 2. launchSessionGuard /auth/launch: context-aware Secure attribute.
	for _, useTLS := range []bool{false, true} {
		sessionTok := "test-session-tok"
		launchSec := "test-launch-sec"
		handler := launchSessionGuard(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}), sessionTok, launchSec, false)

		targetURL := "http://127.0.0.1:18088/auth/launch?ticket=" + launchSec
		if useTLS {
			targetURL = "https://127.0.0.1:18088/auth/launch?ticket=" + launchSec
		}
		req := httptest.NewRequest(http.MethodGet, targetURL, nil)
		if useTLS {
			req.TLS = &tls.ConnectionState{}
		}
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusFound {
			t.Fatalf("expected HTTP 302 from /auth/launch (tls=%v), got %d", useTLS, rec.Code)
		}
		c := findCookie(rec.Result().Cookies(), "armax_gui_token")
		if c == nil || !c.HttpOnly || c.SameSite != http.SameSiteStrictMode || c.Secure != useTLS {
			t.Fatalf("cookie attributes mismatch for tls=%v: %+v", useTLS, c)
		}
	}
}
