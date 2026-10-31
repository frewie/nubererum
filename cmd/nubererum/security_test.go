package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func noContent() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
}

func TestValidateSecurityConfigRequiresStrongPasswordOffLoopback(t *testing.T) {
	t.Setenv("APP_PASSWORD", "")
	// compose.yaml passes APP_AUTH=basic and an empty APP_PASSWORD by default.
	t.Setenv("APP_AUTH", "basic")
	if err := validateSecurityConfig("127.0.0.1"); err != nil {
		t.Fatalf("loopback without password: %v", err)
	}
	if err := validateSecurityConfig("0.0.0.0"); err == nil {
		t.Fatal("expected network host without password to be rejected")
	}
	t.Setenv("APP_PASSWORD", "short")
	if err := validateSecurityConfig("0.0.0.0"); err == nil {
		t.Fatal("expected short password to be rejected")
	}
	t.Setenv("APP_PASSWORD", strings.Repeat("x", minAppPasswordLength))
	if err := validateSecurityConfig("0.0.0.0"); err != nil {
		t.Fatalf("strong password rejected: %v", err)
	}
	// Passkey mode needs no password, only its origins.
	t.Setenv("APP_AUTH", "passkey")
	t.Setenv("APP_PASSWORD", "")
	t.Setenv("APP_PASSKEY_ORIGINS", "https://things.example.com")
	if err := validateSecurityConfig("0.0.0.0"); err != nil {
		t.Fatalf("passkey mode without password rejected: %v", err)
	}
}

func TestClientIPUsesNearestProxyEntry(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.RemoteAddr = "172.18.0.5:41000"
	request.Header.Set("X-Forwarded-For", "6.6.6.6, 203.0.113.9")
	if got := clientIP(request); got != "203.0.113.9" {
		t.Fatalf("clientIP = %q, want the proxy-appended address", got)
	}
	request.Header.Del("X-Forwarded-For")
	if got := clientIP(request); got != "172.18.0.5" {
		t.Fatalf("clientIP = %q, want remote address", got)
	}
}

func TestBasicAuthBlocksRepeatedFailures(t *testing.T) {
	t.Setenv("APP_USERNAME", "alex")
	t.Setenv("APP_PASSWORD", "correct horse battery staple")
	limiter := newAuthLimiter()
	now := time.Date(2026, 10, 16, 8, 0, 0, 0, time.UTC)
	limiter.now = func() time.Time { return now }
	protected := basicAuthWithLimiter(noContent(), limiter)

	attempt := func(ip, password string) int {
		request := httptest.NewRequest(http.MethodGet, "/api/sync", nil)
		request.Header.Set("X-Forwarded-For", ip)
		request.SetBasicAuth("alex", password)
		recorder := httptest.NewRecorder()
		protected.ServeHTTP(recorder, request)
		return recorder.Code
	}

	for i := 0; i < authFailuresPerIP; i++ {
		if code := attempt("198.51.100.1", "wrong"); code != http.StatusUnauthorized {
			t.Fatalf("failure %d code = %d, want 401", i+1, code)
		}
	}
	if code := attempt("198.51.100.1", "correct horse battery staple"); code != http.StatusTooManyRequests {
		t.Fatalf("blocked client with correct password code = %d, want 429", code)
	}
	if code := attempt("198.51.100.2", "correct horse battery staple"); code != http.StatusNoContent {
		t.Fatalf("other client code = %d, want 204", code)
	}

	now = now.Add(authBlockDuration + time.Second)
	if code := attempt("198.51.100.1", "correct horse battery staple"); code != http.StatusNoContent {
		t.Fatalf("client after block expiry code = %d, want 204", code)
	}

	request := httptest.NewRequest(http.MethodGet, "/", nil)
	recorder := httptest.NewRecorder()
	protected.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusUnauthorized || recorder.Header().Get("WWW-Authenticate") == "" {
		t.Fatalf("anonymous request code = %d, want 401 challenge", recorder.Code)
	}
}

func TestBasicAuthTightensUnderDistributedAttack(t *testing.T) {
	limiter := newAuthLimiter()
	for i := 0; i < authGlobalFailures; i++ {
		limiter.fail("203.0.113." + string(rune('a'+i%26)) + string(rune('a'+i/26)))
	}
	if !limiter.fail("192.0.2.77") {
		t.Fatal("expected a single failure to block a client during a distributed attack")
	}
}

func TestSameOriginGuardRequiresOriginAndJSON(t *testing.T) {
	protected := sameOriginGuard(noContent())
	post := func(headers map[string]string) int {
		request := httptest.NewRequest(http.MethodPost, "/api/complete", strings.NewReader(`{}`))
		request.Host = "things.example.org"
		for key, value := range headers {
			request.Header.Set(key, value)
		}
		recorder := httptest.NewRecorder()
		protected.ServeHTTP(recorder, request)
		return recorder.Code
	}

	if code := post(map[string]string{"Content-Type": "application/json"}); code != http.StatusForbidden {
		t.Fatalf("missing origin code = %d, want 403", code)
	}
	if code := post(map[string]string{"Origin": "https://things.example.org", "Content-Type": "text/plain"}); code != http.StatusUnsupportedMediaType {
		t.Fatalf("form content type code = %d, want 415", code)
	}
	if code := post(map[string]string{"Origin": "https://things.example.org", "Content-Type": "application/json", "Sec-Fetch-Site": "cross-site"}); code != http.StatusForbidden {
		t.Fatalf("cross-site fetch code = %d, want 403", code)
	}
	if code := post(map[string]string{"Origin": "https://things.example.org", "Content-Type": "application/json; charset=utf-8", "Sec-Fetch-Site": "same-origin"}); code != http.StatusNoContent {
		t.Fatalf("same-origin JSON code = %d, want 204", code)
	}

	request := httptest.NewRequest(http.MethodDelete, "/api/complete", nil)
	recorder := httptest.NewRecorder()
	protected.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusMethodNotAllowed {
		t.Fatalf("DELETE code = %d, want 405", recorder.Code)
	}
}

func TestSameOriginGuardAcceptsConfiguredPublicOrigin(t *testing.T) {
	t.Setenv("APP_PUBLIC_ORIGINS", "https://things.example.org, https://tunnel.example.net")
	protected := sameOriginGuard(noContent())
	request := httptest.NewRequest(http.MethodPost, "/api/complete", strings.NewReader(`{}`))
	request.Host = "127.0.0.1:3006"
	request.Header.Set("Origin", "https://tunnel.example.net")
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	protected.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("configured origin code = %d, want 204", recorder.Code)
	}
}

func TestHTTPSOnlyRedirectsPlainProxyRequests(t *testing.T) {
	protected := httpsOnly(noContent())
	request := httptest.NewRequest(http.MethodGet, "/api/sync?x=1", nil)
	request.Host = "things.example.org"
	request.Header.Set("X-Forwarded-Proto", "http")
	recorder := httptest.NewRecorder()
	protected.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusPermanentRedirect || recorder.Header().Get("Location") != "https://things.example.org/api/sync?x=1" {
		t.Fatalf("redirect = %d %q", recorder.Code, recorder.Header().Get("Location"))
	}
}

func TestSecurityHeaders(t *testing.T) {
	protected := securityHeaders(noContent())
	request := httptest.NewRequest(http.MethodGet, "/api/sync", nil)
	request.Header.Set("X-Forwarded-Proto", "https")
	recorder := httptest.NewRecorder()
	protected.ServeHTTP(recorder, request)
	for _, header := range []string{"Strict-Transport-Security", "Content-Security-Policy", "X-Frame-Options", "Permissions-Policy", "Cross-Origin-Opener-Policy"} {
		if recorder.Header().Get(header) == "" {
			t.Errorf("missing %s", header)
		}
	}
	if got := recorder.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("API Cache-Control = %q, want no-store", got)
	}
}

func TestCLIInputValidation(t *testing.T) {
	for _, id := range []string{"Tuj9Umm6EZ5FEeXJdKZgux", "CC6BD6E2-8C01-46B6-BA68-6A4BFED5C65E"} {
		if !validThingsID(id) {
			t.Errorf("valid ID %q rejected", id)
		}
	}
	for _, id := range []string{"", "--uuid", "-x", "a b", "a;b", strings.Repeat("a", 65)} {
		if validThingsID(id) {
			t.Errorf("invalid ID %q accepted", id)
		}
	}
	if safeCLIValue("--dry-run") || safeCLIValue("  --uuid") {
		t.Error("expected option-like values to be rejected")
	}
	if !safeCLIValue("- buy milk") {
		t.Error("expected a single leading dash to be allowed")
	}
}

func TestLoginDisabledWhenServerManagesCredentials(t *testing.T) {
	previous := serverManagedCredentials
	serverManagedCredentials = true
	t.Cleanup(func() { serverManagedCredentials = previous })

	for _, path := range []string{"/api/login", "/api/logout"} {
		request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"email":"a@b.c","password":"x"}`))
		recorder := httptest.NewRecorder()
		apiHandler(recorder, request)
		if recorder.Code != http.StatusForbidden {
			t.Errorf("%s code = %d, want 403", path, recorder.Code)
		}
	}
}
