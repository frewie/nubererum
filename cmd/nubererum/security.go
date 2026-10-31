package main

import (
	"crypto/subtle"
	"fmt"
	"log"
	"mime"
	"net"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	minAppPasswordLength = 16
	maxRequestBodyBytes  = 64 << 10

	authFailureWindow       = 15 * time.Minute
	authFailuresPerIP       = 5
	authBlockDuration       = 15 * time.Minute
	authGlobalFailures      = 50
	authTrackedClients      = 10000
	authFailuresUnderAttack = 1
)

var thingsIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9-]{0,63}$`)

// serverManagedCredentials is true when the Things account comes from the
// environment. The browser cannot then switch or clear the account.
var serverManagedCredentials = session.Email != "" && session.Password != ""

func isLoopbackHost(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// validateSecurityConfig refuses to start an app reachable from the network
// without a strong Basic Auth password, and a passkey app without a valid
// origin.
func validateSecurityConfig(host string) error {
	switch authMode() {
	case "passkey":
		_, err := passkeyOrigins()
		return err
	case "basic":
	default:
		return fmt.Errorf("APP_AUTH must be basic or passkey, not %q", os.Getenv("APP_AUTH"))
	}
	if isLoopbackHost(host) {
		return nil
	}
	password := os.Getenv("APP_PASSWORD")
	if password == "" {
		return fmt.Errorf("APP_PASSWORD is required when HOST=%s is reachable from the network", host)
	}
	if len(password) < minAppPasswordLength {
		return fmt.Errorf("APP_PASSWORD must be at least %d characters", minAppPasswordLength)
	}
	return nil
}

// clientIP returns the address of the client as seen by the nearest proxy.
// Proxies append the peer address to X-Forwarded-For, so the rightmost entry
// is the one a client cannot forge.
func clientIP(r *http.Request) string {
	if forwarded := r.Header.Values("X-Forwarded-For"); len(forwarded) > 0 {
		parts := strings.Split(strings.Join(forwarded, ","), ",")
		for i := len(parts) - 1; i >= 0; i-- {
			if candidate := strings.TrimSpace(parts[i]); candidate != "" {
				return candidate
			}
		}
	}
	if realIP := strings.TrimSpace(r.Header.Get("X-Real-IP")); realIP != "" {
		return realIP
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

type authAttempts struct {
	failures     int
	windowStart  time.Time
	blockedUntil time.Time
}

// authLimiter blocks clients after repeated Basic Auth failures. When many
// failures arrive from many addresses, a single failure blocks a client.
type authLimiter struct {
	mu             sync.Mutex
	now            func() time.Time
	clients        map[string]*authAttempts
	globalFailures []time.Time
}

func newAuthLimiter() *authLimiter {
	return &authLimiter{now: time.Now, clients: map[string]*authAttempts{}}
}

func (l *authLimiter) blocked(ip string) (time.Duration, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	entry, ok := l.clients[ip]
	if !ok {
		return 0, false
	}
	if remaining := entry.blockedUntil.Sub(l.now()); remaining > 0 {
		return remaining, true
	}
	return 0, false
}

func (l *authLimiter) fail(ip string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	l.prune(now)

	l.globalFailures = append(l.globalFailures, now)
	limit := authFailuresPerIP
	if len(l.globalFailures) >= authGlobalFailures {
		limit = authFailuresUnderAttack
	}

	entry, ok := l.clients[ip]
	if !ok || now.Sub(entry.windowStart) > authFailureWindow {
		entry = &authAttempts{windowStart: now}
		l.clients[ip] = entry
	}
	entry.failures++
	if entry.failures >= limit {
		entry.blockedUntil = now.Add(authBlockDuration)
		return true
	}
	return false
}

func (l *authLimiter) succeed(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.clients, ip)
}

func (l *authLimiter) prune(now time.Time) {
	kept := l.globalFailures[:0]
	for _, at := range l.globalFailures {
		if now.Sub(at) <= authFailureWindow {
			kept = append(kept, at)
		}
	}
	l.globalFailures = kept

	if len(l.clients) < authTrackedClients {
		return
	}
	for ip, entry := range l.clients {
		if now.After(entry.blockedUntil) && now.Sub(entry.windowStart) > authFailureWindow {
			delete(l.clients, ip)
		}
	}
	if len(l.clients) >= authTrackedClients {
		// Still full of active entries: keep blocked clients, drop the rest.
		for ip, entry := range l.clients {
			if now.After(entry.blockedUntil) {
				delete(l.clients, ip)
			}
		}
	}
}

func basicAuth(next http.Handler) http.Handler {
	return basicAuthWithLimiter(next, newAuthLimiter())
}

func basicAuthWithLimiter(next http.Handler, limiter *authLimiter) http.Handler {
	password := os.Getenv("APP_PASSWORD")
	if password == "" {
		return next
	}
	username := firstEnv("APP_USERNAME")
	if username == "" {
		username = "admin"
	}
	realm := `Basic realm="NubeRerum", charset="UTF-8"`
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := clientIP(r)
		if remaining, blocked := limiter.blocked(ip); blocked {
			w.Header().Set("Retry-After", strconv.Itoa(int(remaining.Seconds())+1))
			http.Error(w, "Too many failed sign-in attempts. Try again later.", http.StatusTooManyRequests)
			return
		}
		user, pass, ok := r.BasicAuth()
		userOK := subtle.ConstantTimeCompare([]byte(user), []byte(username)) == 1
		passOK := subtle.ConstantTimeCompare([]byte(pass), []byte(password)) == 1
		if ok && userOK && passOK {
			limiter.succeed(ip)
			next.ServeHTTP(w, r)
			return
		}
		if ok {
			if limiter.fail(ip) {
				log.Printf("auth: blocked %s for %s after failed sign-in attempts", ip, authBlockDuration)
			} else {
				log.Printf("auth: failed sign-in from %s", ip)
			}
		}
		w.Header().Set("WWW-Authenticate", realm)
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
	})
}

// httpsOnly redirects requests that a proxy received over plain HTTP, so Basic
// Auth credentials are never requested without TLS.
func httpsOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "http") {
			target := "https://" + r.Host + r.URL.RequestURI()
			http.Redirect(w, r, target, http.StatusPermanentRedirect)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Content-Security-Policy", "default-src 'self'; style-src 'self' 'unsafe-inline'; script-src 'self'; img-src 'self' data:; connect-src 'self'; form-action 'self'; object-src 'none'; base-uri 'none'; frame-ancestors 'none'")
		h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=(), payment=(), usb=(), interest-cohort=()")
		h.Set("Cross-Origin-Opener-Policy", "same-origin")
		h.Set("Cross-Origin-Resource-Policy", "same-origin")
		h.Set("X-Robots-Tag", "noindex, nofollow")
		if strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
			h.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		}
		if strings.HasPrefix(r.URL.Path, "/api/") {
			h.Set("Cache-Control", "no-store")
		} else {
			h.Set("Cache-Control", "private, no-cache")
		}
		next.ServeHTTP(w, r)
	})
}

// allowedOrigins lists extra public origins (APP_PUBLIC_ORIGINS, comma
// separated) for proxies that do not pass the original Host header.
func allowedOrigins() map[string]bool {
	result := map[string]bool{}
	for _, origin := range strings.Split(os.Getenv("APP_PUBLIC_ORIGINS"), ",") {
		if parsed, err := url.Parse(strings.TrimSpace(origin)); err == nil && parsed.Host != "" {
			result[strings.ToLower(parsed.Host)] = true
		}
	}
	return result
}

// sameOriginGuard blocks cross-site state changes. Browsers always send Origin
// on POST, and the app only accepts JSON, which a cross-site form cannot send.
func sameOriginGuard(next http.Handler) http.Handler {
	extra := allowedOrigins()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet || r.Method == http.MethodHead {
			next.ServeHTTP(w, r)
			return
		}
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if site := r.Header.Get("Sec-Fetch-Site"); site != "" && site != "same-origin" && site != "none" {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
		source := r.Header.Get("Origin")
		if source == "" {
			source = r.Header.Get("Referer")
		}
		parsed, err := url.Parse(source)
		if source == "" || err != nil || parsed.Host == "" {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
		host := strings.ToLower(parsed.Host)
		if host != strings.ToLower(r.Host) && !extra[host] {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
		if mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type")); err != nil || mediaType != "application/json" {
			http.Error(w, "Unsupported media type", http.StatusUnsupportedMediaType)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
		next.ServeHTTP(w, r)
	})
}

func validThingsID(id string) bool {
	return thingsIDPattern.MatchString(id)
}

// safeCLIValue rejects values the CLI would parse as an option.
func safeCLIValue(value string) bool {
	return !strings.HasPrefix(strings.TrimSpace(value), "--")
}

func newHTTPServer(address string, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              address,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		// The first sync of a large Things history can take minutes.
		WriteTimeout:   10 * time.Minute,
		IdleTimeout:    2 * time.Minute,
		MaxHeaderBytes: 32 << 10,
	}
}

// appHandler wraps the routes in the middleware of the configured auth mode.
// In passkey mode the same-origin guard also covers the /auth/ endpoints.
// /mcp is routed before the auth middlewares and the same-origin guard: it
// accepts Bearer tokens only (mcp.go) and never a session or password.
func appHandler(mux http.Handler) http.Handler {
	mcp := newMCPServer(passkeyDataDir(), newAuthLimiter())
	if authMode() == "passkey" {
		if os.Getenv("APP_PASSWORD") != "" {
			log.Print("auth: APP_PASSWORD is ignored because APP_AUTH=passkey")
		}
		passkeys, err := newPasskeyServer(passkeyDataDir(), newAuthLimiter())
		if err != nil {
			log.Fatal(err)
		}
		return securityHeaders(mcp.route(httpsOnly(gzipResponses(logErrors(sameOriginGuard(passkeys.middleware(mux)))))))
	}
	return securityHeaders(mcp.route(httpsOnly(basicAuth(gzipResponses(logErrors(sameOriginGuard(mux)))))))
}
