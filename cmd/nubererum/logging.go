package main

import (
	"bytes"
	"log"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

// Logs go to stderr as key=value lines. Docker keeps them; ship container
// logs to your log system to search them, e.g. for "level=ERROR".
func setupLogging() {
	handler := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo})
	slog.SetDefault(slog.New(handler))
	// log.Printf calls elsewhere become INFO records.
	log.SetFlags(0)
}

// redact removes the Things account from log text: Things Cloud errors
// contain the email address in URLs.
func redact(text string) string {
	sessionMu.RLock()
	email, password := session.Email, session.Password
	sessionMu.RUnlock()
	for _, secret := range []string{password, email, url.PathEscape(email), url.QueryEscape(email)} {
		if len(secret) >= 3 {
			text = strings.ReplaceAll(text, secret, "<account>")
		}
	}
	return text
}

func logError(message string, err error, args ...any) {
	slog.Error(message, append(args, "error", redact(err.Error()))...)
}

// errorRecorder captures the status and, for errors, the start of the body.
type errorRecorder struct {
	http.ResponseWriter
	status int
	body   bytes.Buffer
}

func (r *errorRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func (r *errorRecorder) Write(data []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	if r.status >= 400 && r.body.Len() < 2048 {
		r.body.Write(data[:min(len(data), 2048-r.body.Len())])
	}
	return r.ResponseWriter.Write(data)
}

// slowRequest is the duration from which successful requests are logged.
const slowRequest = 3 * time.Second

// logErrors logs every failed API and auth request: 5xx as errors, other
// failures as warnings. An expected 401 for a missing session is not logged.
// Successful requests are logged when they take slowRequest or longer.
func logErrors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/api/") && !strings.HasPrefix(r.URL.Path, "/auth/") {
			next.ServeHTTP(w, r)
			return
		}
		started := time.Now()
		recorder := &errorRecorder{ResponseWriter: w}
		defer func() {
			if recovered := recover(); recovered != nil {
				slog.Error("panic", "method", r.Method, "path", r.URL.Path, "panic", redact(strings.TrimSpace(fmtAny(recovered))))
				if recorder.status == 0 {
					writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "Internal error."})
				}
			}
		}()
		next.ServeHTTP(recorder, r)
		if duration := time.Since(started); recorder.status < 400 && duration >= slowRequest {
			slog.Info("slow request", "method", r.Method, "path", r.URL.Path, "status", recorder.status, "duration_ms", duration.Milliseconds())
		}
		if recorder.status < 400 || recorder.status == http.StatusUnauthorized {
			return
		}
		level := slog.LevelWarn
		if recorder.status >= 500 {
			level = slog.LevelError
		}
		slog.Log(r.Context(), level, "request failed",
			"method", r.Method,
			"path", r.URL.Path,
			"status", recorder.status,
			"duration_ms", time.Since(started).Milliseconds(),
			"client", clientIP(r),
			"response", redact(strings.TrimSpace(recorder.body.String())),
		)
	})
}

func fmtAny(value any) string {
	if err, ok := value.(error); ok {
		return err.Error()
	}
	if text, ok := value.(string); ok {
		return text
	}
	return slog.AnyValue(value).String()
}

var clientReports = struct {
	sync.Mutex
	count  int
	window time.Time
}{}

// handleClientError serves POST /api/client-error: the page reports its
// JavaScript errors and failed requests here, so they reach the logs too. At
// most 60 reports per minute are logged.
func handleClientError(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Message string `json:"message"`
		Source  string `json:"source"`
		Line    int    `json:"line"`
		Stack   string `json:"stack"`
		Context string `json:"context"`
		Page    string `json:"page"`
	}
	if err := readJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Invalid report."})
		return
	}
	clientReports.Lock()
	if time.Since(clientReports.window) > time.Minute {
		clientReports.window, clientReports.count = time.Now(), 0
	}
	clientReports.count++
	allowed := clientReports.count <= 60
	clientReports.Unlock()
	if allowed {
		clip := func(value string, max int) string {
			value = redact(strings.TrimSpace(value))
			if len(value) > max {
				value = value[:max] + "…"
			}
			return value
		}
		slog.Warn("client error",
			"message", clip(body.Message, 500),
			"context", clip(body.Context, 200),
			"source", clip(body.Source, 200),
			"line", body.Line,
			"page", clip(body.Page, 200),
			"stack", clip(body.Stack, 2000),
			"client", clientIP(r),
			"agent", clip(r.UserAgent(), 200),
		)
	}
	w.WriteHeader(http.StatusNoContent)
}
