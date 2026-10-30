package main

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base32"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
)

const (
	passkeyCookieName     = "__Host-things_session"
	passkeySessionTTL     = 30 * 24 * time.Hour
	passkeyCeremonyTTL    = 5 * time.Minute
	passkeyMaxCeremonies  = 64
	passkeySetupCodeTTL   = 30 * time.Minute
	passkeyStoreFileName  = "passkeys.json"
	passkeySetupFileName  = "passkey-setup.json"
	maxPasskeyNameLength  = 64
	passkeyUserHandleSize = 32
)

// authMode is "basic" (default, for local and LAN use) or "passkey".
func authMode() string {
	mode := strings.ToLower(strings.TrimSpace(os.Getenv("APP_AUTH")))
	if mode == "" {
		return "basic"
	}
	return mode
}

// passkeyOrigins are the origins passkeys work on: APP_PASSKEY_ORIGINS (or
// APP_PASSKEY_ORIGIN), or else every entry of APP_PUBLIC_ORIGINS. Each origin
// is its own relying party, so a passkey works only on the origin it was
// registered on.
func passkeyOrigins() ([]*url.URL, error) {
	value := firstEnv("APP_PASSKEY_ORIGINS", "APP_PASSKEY_ORIGIN", "APP_PUBLIC_ORIGINS")
	var origins []*url.URL
	seen := map[string]bool{}
	for _, entry := range strings.Split(value, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		origin, err := url.Parse(entry)
		if err != nil || origin.Hostname() == "" || (origin.Path != "" && origin.Path != "/") || origin.RawQuery != "" || origin.User != nil {
			return nil, fmt.Errorf("passkey origin %q must look like https://host[:port]", entry)
		}
		if origin.Scheme != "https" && !(origin.Scheme == "http" && origin.Hostname() == "localhost") {
			return nil, fmt.Errorf("passkey origin %q must use https (http is only allowed for localhost)", entry)
		}
		origin.Path = ""
		origin.Host = strings.ToLower(origin.Host)
		if key := originKey(origin); !seen[key] {
			seen[key] = true
			origins = append(origins, origin)
		}
	}
	if len(origins) == 0 {
		return nil, errors.New("APP_AUTH=passkey needs APP_PASSKEY_ORIGINS or APP_PUBLIC_ORIGINS, e.g. https://things.example.com")
	}
	return origins, nil
}

func originKey(origin *url.URL) string {
	return origin.Scheme + "://" + strings.ToLower(origin.Host)
}

func passkeyDataDir() string {
	if dir := firstEnv("DATA_DIR"); dir != "" {
		return dir
	}
	return appRoot()
}

func encodeCredentialID(id []byte) string {
	return base64.RawURLEncoding.EncodeToString(id)
}

func randomBytes(size int) []byte {
	buffer := make([]byte, size)
	if _, err := rand.Read(buffer); err != nil {
		panic(err)
	}
	return buffer
}

func randomToken() string {
	return base64.RawURLEncoding.EncodeToString(randomBytes(32))
}

func hashSecret(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

// writeFileAtomic writes data with mode 0600 so a crash never leaves a
// truncated file.
func writeFileAtomic(path string, data []byte) error {
	temp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	defer os.Remove(temp.Name())
	if err := temp.Chmod(0o600); err != nil {
		temp.Close()
		return err
	}
	if _, err := temp.Write(data); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Sync(); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(temp.Name(), path)
}

// Passkey storage

type storedPasskey struct {
	Name string `json:"name"`
	// RPID is the relying party (host name) the passkey belongs to. Passkeys
	// stored before multi-origin support have none and belong to the first
	// configured origin.
	RPID       string              `json:"rpId,omitempty"`
	CreatedAt  time.Time           `json:"createdAt"`
	LastUsedAt time.Time           `json:"lastUsedAt,omitzero"`
	Credential webauthn.Credential `json:"credential"`
}

func (p storedPasskey) ID() string {
	return encodeCredentialID(p.Credential.ID)
}

type passkeyData struct {
	UserHandle []byte          `json:"userHandle,omitempty"`
	Passkeys   []storedPasskey `json:"passkeys"`
}

func (d *passkeyData) find(id []byte) int {
	for i, passkey := range d.Passkeys {
		if subtle.ConstantTimeCompare(passkey.Credential.ID, id) == 1 {
			return i
		}
	}
	return -1
}

// passkeyStore keeps the single owner's WebAuthn user handle and credentials
// in DATA_DIR/passkeys.json. It is read on every use, so changes made by the
// CLI subcommands apply to the running server immediately.
type passkeyStore struct {
	mu   sync.Mutex
	path string
}

func newPasskeyStore(dir string) *passkeyStore {
	return &passkeyStore{path: filepath.Join(dir, passkeyStoreFileName)}
}

func (s *passkeyStore) load() (passkeyData, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.read()
}

func (s *passkeyStore) read() (passkeyData, error) {
	var data passkeyData
	raw, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return data, nil
	}
	if err != nil {
		return data, err
	}
	if err := json.Unmarshal(raw, &data); err != nil {
		return data, fmt.Errorf("read %s: %w", s.path, err)
	}
	return data, nil
}

func (s *passkeyStore) update(change func(*passkeyData) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := s.read()
	if err != nil {
		return err
	}
	if err := change(&data); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(s.path, append(raw, '\n'))
}

// ensureUserHandle returns the stored WebAuthn user handle, creating it on the
// first registration.
func (s *passkeyStore) ensureUserHandle() (passkeyData, error) {
	var result passkeyData
	err := s.update(func(data *passkeyData) error {
		if len(data.UserHandle) == 0 {
			data.UserHandle = randomBytes(passkeyUserHandleSize)
		}
		result = *data
		return nil
	})
	return result, err
}

// removePasskey removes the passkey whose ID equals or uniquely starts with
// prefix and returns it.
func (s *passkeyStore) removePasskey(prefix string) (storedPasskey, error) {
	var removed storedPasskey
	err := s.update(func(data *passkeyData) error {
		match := -1
		for i, passkey := range data.Passkeys {
			if id := passkey.ID(); id == prefix {
				match = i
				break
			} else if len(prefix) >= 6 && strings.HasPrefix(id, prefix) {
				if match >= 0 {
					return fmt.Errorf("passkey ID %q is ambiguous", prefix)
				}
				match = i
			}
		}
		if match < 0 {
			return fmt.Errorf("no passkey with ID %q", prefix)
		}
		removed = data.Passkeys[match]
		data.Passkeys = append(data.Passkeys[:match], data.Passkeys[match+1:]...)
		return nil
	})
	return removed, err
}

type passkeyUser struct {
	data passkeyData
}

func (u passkeyUser) WebAuthnID() []byte { return u.data.UserHandle }

func (u passkeyUser) WebAuthnName() string {
	if name := firstEnv("APP_USERNAME"); name != "" {
		return name
	}
	return "owner"
}

func (u passkeyUser) WebAuthnDisplayName() string { return "NubeRerum" }

func (u passkeyUser) WebAuthnCredentials() []webauthn.Credential {
	credentials := make([]webauthn.Credential, 0, len(u.data.Passkeys))
	for _, passkey := range u.data.Passkeys {
		credentials = append(credentials, passkey.Credential)
	}
	return credentials
}

// Setup codes

type setupCodeRecord struct {
	Hash      string    `json:"hash"`
	ExpiresAt time.Time `json:"expiresAt"`
}

func normalizeSetupCode(code string) string {
	return strings.Map(func(r rune) rune {
		if r == '-' || unicode.IsSpace(r) {
			return -1
		}
		return unicode.ToUpper(r)
	}, code)
}

// createSetupCode writes a one-time registration code (stored hashed) and
// returns it. A new code replaces any earlier one.
func createSetupCode(dir string, now time.Time) (string, time.Time, error) {
	raw := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(randomBytes(10))
	expires := now.Add(passkeySetupCodeTTL)
	record, err := json.Marshal(setupCodeRecord{Hash: hashSecret(raw), ExpiresAt: expires})
	if err != nil {
		return "", time.Time{}, err
	}
	if err := writeFileAtomic(filepath.Join(dir, passkeySetupFileName), record); err != nil {
		return "", time.Time{}, err
	}
	code := raw[0:4] + "-" + raw[4:8] + "-" + raw[8:12] + "-" + raw[12:16]
	return code, expires, nil
}

func setupCodeValid(dir, code string, now time.Time) bool {
	code = normalizeSetupCode(code)
	if code == "" {
		return false
	}
	raw, err := os.ReadFile(filepath.Join(dir, passkeySetupFileName))
	if err != nil {
		return false
	}
	var record setupCodeRecord
	if json.Unmarshal(raw, &record) != nil || !now.Before(record.ExpiresAt) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(hashSecret(code)), []byte(record.Hash)) == 1
}

func consumeSetupCode(dir, code string, now time.Time) bool {
	if !setupCodeValid(dir, code, now) {
		return false
	}
	return os.Remove(filepath.Join(dir, passkeySetupFileName)) == nil
}

// Server

type authSession struct {
	credentialID []byte
	expires      time.Time
}

type passkeyCeremony struct {
	origin    string
	register  bool
	data      webauthn.SessionData
	setupCode string
	name      string
	expires   time.Time
}

type passkeyServer struct {
	// parties holds one relying party per origin, keyed by originKey.
	parties     map[string]*webauthn.WebAuthn
	primaryRPID string
	store       *passkeyStore
	dir         string
	limiter     *authLimiter
	now         func() time.Time
	public      http.Handler

	mu         sync.Mutex
	sessions   map[string]authSession
	ceremonies map[string]passkeyCeremony
}

func newPasskeyServer(dir string, limiter *authLimiter) (*passkeyServer, error) {
	origins, err := passkeyOrigins()
	if err != nil {
		return nil, err
	}
	parties := map[string]*webauthn.WebAuthn{}
	for _, origin := range origins {
		party, err := newRelyingParty(origin)
		if err != nil {
			return nil, err
		}
		parties[originKey(origin)] = party
	}
	return &passkeyServer{
		parties:     parties,
		primaryRPID: origins[0].Hostname(),
		store:       newPasskeyStore(dir),
		dir:         dir,
		limiter:     limiter,
		now:         time.Now,
		sessions:    map[string]authSession{},
		ceremonies:  map[string]passkeyCeremony{},
	}, nil
}

func newRelyingParty(origin *url.URL) (*webauthn.WebAuthn, error) {
	return webauthn.New(&webauthn.Config{
		RPID:          origin.Hostname(),
		RPDisplayName: "NubeRerum",
		RPOrigins:     []string{originKey(origin)},
		AuthenticatorSelection: protocol.AuthenticatorSelection{
			ResidentKey:        protocol.ResidentKeyRequirementRequired,
			RequireResidentKey: protocol.ResidentKeyRequired(),
			UserVerification:   protocol.VerificationRequired,
		},
		Timeouts: webauthn.TimeoutsConfig{
			Login:        webauthn.TimeoutConfig{Enforce: true, Timeout: passkeyCeremonyTTL, TimeoutUVD: passkeyCeremonyTTL},
			Registration: webauthn.TimeoutConfig{Enforce: true, Timeout: passkeyCeremonyTTL, TimeoutUVD: passkeyCeremonyTTL},
		},
	})
}

// requestOrigin returns the origin the browser reports for a request: Origin,
// or the origin of Referer.
func requestOrigin(r *http.Request) string {
	source := r.Header.Get("Origin")
	if source == "" {
		source = r.Header.Get("Referer")
	}
	parsed, err := url.Parse(source)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return ""
	}
	return originKey(parsed)
}

// relyingParty picks the relying party for the origin of r.
func (s *passkeyServer) relyingParty(r *http.Request) (string, *webauthn.WebAuthn, bool) {
	origin := requestOrigin(r)
	party, ok := s.parties[origin]
	return origin, party, ok
}

// rpidFor returns the relying party a stored passkey belongs to.
func (s *passkeyServer) rpidFor(passkey storedPasskey) string {
	if passkey.RPID != "" {
		return passkey.RPID
	}
	return s.primaryRPID
}

func writeUnknownOrigin(w http.ResponseWriter) {
	writeJSON(w, http.StatusForbidden, map[string]any{"error": "Passkeys are not enabled for this address."})
}

// publicPaths are served without a session: the login page and its assets.
var publicPaths = map[string]bool{
	"/login":                 true,
	"/login.js":              true,
	"/styles.css":            true,
	"/manifest.webmanifest":  true,
	"/icon.svg":              true,
	"/icon-192.png":          true,
	"/icon-512.png":          true,
	"/icon-maskable-512.png": true,
	"/apple-touch-icon.png":  true,
	"/favicon-32.png":        true,
}

// middleware serves /auth/* and /login, and lets other requests through only
// with a valid session. Unauthenticated API calls get 401, pages redirect.
func (s *passkeyServer) middleware(next http.Handler) http.Handler {
	routes := http.NewServeMux()
	routes.HandleFunc("GET /auth/status", s.handleStatus)
	routes.HandleFunc("POST /auth/login/begin", s.handleLoginBegin)
	routes.HandleFunc("POST /auth/login/finish", s.handleLoginFinish)
	routes.HandleFunc("POST /auth/register/begin", s.handleRegisterBegin)
	routes.HandleFunc("POST /auth/register/finish", s.handleRegisterFinish)
	routes.HandleFunc("POST /auth/logout", s.handleLogout)
	routes.HandleFunc("/auth/", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "Not found."})
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		switch {
		case strings.HasPrefix(path, "/auth/"):
			w.Header().Set("Cache-Control", "no-store")
			routes.ServeHTTP(w, r)
		case path == "/login":
			serveLoginPage(w, r)
		case publicPaths[path]:
			next.ServeHTTP(w, r)
		case s.authenticated(r):
			next.ServeHTTP(w, r)
		case strings.HasPrefix(path, "/api/"):
			writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "Sign in with your passkey.", "login": "/login"})
		case r.Method == http.MethodGet || r.Method == http.MethodHead:
			http.Redirect(w, r, "/login", http.StatusSeeOther)
		default:
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
		}
	})
}

func serveLoginPage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	data, err := os.ReadFile(filepath.Join(appRoot(), "public", "login.html"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("content-type", "text/html; charset=utf-8")
	_, _ = w.Write(data)
}

// session returns the session of the request if it is unexpired and its
// passkey still exists, so removing a passkey ends its sessions.
func (s *passkeyServer) session(r *http.Request) (string, authSession, bool) {
	cookie, err := r.Cookie(passkeyCookieName)
	if err != nil || cookie.Value == "" {
		return "", authSession{}, false
	}
	key := hashSecret(cookie.Value)
	s.mu.Lock()
	current, ok := s.sessions[key]
	if ok && !s.now().Before(current.expires) {
		delete(s.sessions, key)
		ok = false
	}
	s.mu.Unlock()
	if !ok {
		return "", authSession{}, false
	}
	data, err := s.store.load()
	if err != nil {
		log.Printf("auth: %v", err)
		return "", authSession{}, false
	}
	if data.find(current.credentialID) < 0 {
		s.mu.Lock()
		delete(s.sessions, key)
		s.mu.Unlock()
		return "", authSession{}, false
	}
	return key, current, true
}

func (s *passkeyServer) authenticated(r *http.Request) bool {
	_, _, ok := s.session(r)
	return ok
}

func (s *passkeyServer) startSession(w http.ResponseWriter, credentialID []byte) {
	token := randomToken()
	now := s.now()
	s.mu.Lock()
	for key, existing := range s.sessions {
		if !now.Before(existing.expires) {
			delete(s.sessions, key)
		}
	}
	s.sessions[hashSecret(token)] = authSession{credentialID: credentialID, expires: now.Add(passkeySessionTTL)}
	s.mu.Unlock()
	http.SetCookie(w, &http.Cookie{
		Name:     passkeyCookieName,
		Value:    token,
		Path:     "/",
		MaxAge:   int(passkeySessionTTL.Seconds()),
		Secure:   true,
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	})
}

func clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     passkeyCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		Secure:   true,
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	})
}

// addCeremony stores a pending WebAuthn ceremony and returns its ID. When the
// table is full, the oldest ceremony is dropped.
func (s *passkeyServer) addCeremony(ceremony passkeyCeremony) string {
	id := randomToken()
	now := s.now()
	ceremony.expires = now.Add(passkeyCeremonyTTL)
	s.mu.Lock()
	defer s.mu.Unlock()
	oldestID := ""
	for key, existing := range s.ceremonies {
		if !now.Before(existing.expires) {
			delete(s.ceremonies, key)
			continue
		}
		if oldestID == "" || existing.expires.Before(s.ceremonies[oldestID].expires) {
			oldestID = key
		}
	}
	if len(s.ceremonies) >= passkeyMaxCeremonies && oldestID != "" {
		delete(s.ceremonies, oldestID)
	}
	s.ceremonies[id] = ceremony
	return id
}

// takeCeremony removes and returns a ceremony, so each can finish only once.
func (s *passkeyServer) takeCeremony(id string, register bool) (passkeyCeremony, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ceremony, ok := s.ceremonies[id]
	if !ok {
		return passkeyCeremony{}, false
	}
	delete(s.ceremonies, id)
	if ceremony.register != register || !s.now().Before(ceremony.expires) {
		return passkeyCeremony{}, false
	}
	return ceremony, true
}

// rejectBlocked answers 429 when the client is locked out after failures.
func (s *passkeyServer) rejectBlocked(w http.ResponseWriter, ip string) bool {
	remaining, blocked := s.limiter.blocked(ip)
	if !blocked {
		return false
	}
	w.Header().Set("Retry-After", strconv.Itoa(int(remaining.Seconds())+1))
	writeJSON(w, http.StatusTooManyRequests, map[string]any{"error": "Too many failed sign-in attempts. Try again later."})
	return true
}

func (s *passkeyServer) recordFailure(ip, what string, err error) {
	if s.limiter.fail(ip) {
		log.Printf("auth: blocked %s for %s after failed %s: %v", ip, authBlockDuration, what, err)
	} else {
		log.Printf("auth: failed %s from %s: %v", what, ip, err)
	}
}

func cleanPasskeyName(name string, now time.Time) string {
	name = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, strings.TrimSpace(name))
	if runes := []rune(name); len(runes) > maxPasskeyNameLength {
		name = string(runes[:maxPasskeyNameLength])
	}
	if name == "" {
		name = "Passkey " + now.Format("2006-01-02")
	}
	return name
}

type ceremonyResponse struct {
	Ceremony   string          `json:"ceremony"`
	Credential json.RawMessage `json:"credential"`
}

func readCeremonyResponse(r *http.Request) (ceremonyResponse, error) {
	var body ceremonyResponse
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		return body, err
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		return body, err
	}
	if body.Ceremony == "" || len(body.Credential) == 0 {
		return body, errors.New("missing ceremony or credential")
	}
	return body, nil
}

func (s *passkeyServer) handleStatus(w http.ResponseWriter, r *http.Request) {
	data, err := s.store.load()
	if err != nil {
		log.Printf("auth: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "Passkey storage is unreadable."})
		return
	}
	// GET requests carry no Origin, so match the Host header. Without a match
	// (e.g. behind a proxy that rewrites Host), count every passkey.
	registered := 0
	matched := false
	for key, party := range s.parties {
		if strings.EqualFold(strings.SplitN(key, "://", 2)[1], r.Host) {
			matched = true
			for _, passkey := range data.Passkeys {
				if s.rpidFor(passkey) == party.Config.RPID {
					registered++
				}
			}
		}
	}
	if !matched {
		registered = len(data.Passkeys)
	}
	response := map[string]any{
		"mode":          "passkey",
		"authenticated": false,
		"registered":    registered > 0,
	}
	if _, current, ok := s.session(r); ok {
		response["authenticated"] = true
		if index := data.find(current.credentialID); index >= 0 {
			response["passkey"] = data.Passkeys[index].Name
		}
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *passkeyServer) handleLoginBegin(w http.ResponseWriter, r *http.Request) {
	if s.rejectBlocked(w, clientIP(r)) {
		return
	}
	origin, party, ok := s.relyingParty(r)
	if !ok {
		writeUnknownOrigin(w)
		return
	}
	options, data, err := party.BeginDiscoverableLogin(webauthn.WithUserVerification(protocol.VerificationRequired))
	if err != nil {
		log.Printf("auth: begin login: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "Could not start sign-in."})
		return
	}
	id := s.addCeremony(passkeyCeremony{origin: origin, data: *data})
	writeJSON(w, http.StatusOK, map[string]any{"ceremony": id, "options": options})
}

func (s *passkeyServer) handleLoginFinish(w http.ResponseWriter, r *http.Request) {
	ip := clientIP(r)
	if s.rejectBlocked(w, ip) {
		return
	}
	fail := func(err error) {
		s.recordFailure(ip, "passkey sign-in", err)
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "Passkey sign-in failed."})
	}
	body, err := readCeremonyResponse(r)
	if err != nil {
		fail(err)
		return
	}
	ceremony, ok := s.takeCeremony(body.Ceremony, false)
	if !ok {
		fail(errors.New("unknown or expired ceremony"))
		return
	}
	party := s.parties[ceremony.origin]
	if requestOrigin(r) != ceremony.origin || party == nil {
		fail(errors.New("ceremony finished from another origin"))
		return
	}
	parsed, err := protocol.ParseCredentialRequestResponseBytes(body.Credential)
	if err != nil {
		fail(err)
		return
	}
	data, err := s.store.load()
	if err != nil {
		log.Printf("auth: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "Passkey storage is unreadable."})
		return
	}
	lookup := func(rawID, userHandle []byte) (webauthn.User, error) {
		if len(data.UserHandle) == 0 || subtle.ConstantTimeCompare(userHandle, data.UserHandle) != 1 {
			return nil, errors.New("unknown user handle")
		}
		return passkeyUser{data: data}, nil
	}
	_, credential, err := party.ValidatePasskeyLogin(lookup, ceremony.data, parsed)
	if err != nil {
		fail(err)
		return
	}
	if index := data.find(credential.ID); index < 0 || s.rpidFor(data.Passkeys[index]) != party.Config.RPID {
		fail(errors.New("passkey belongs to another origin"))
		return
	}
	if credential.Authenticator.CloneWarning {
		fail(errors.New("signature counter went backwards; the authenticator may be cloned"))
		return
	}
	err = s.store.update(func(current *passkeyData) error {
		index := current.find(credential.ID)
		if index < 0 {
			return errors.New("passkey was removed")
		}
		current.Passkeys[index].Credential = *credential
		current.Passkeys[index].LastUsedAt = s.now().UTC()
		return nil
	})
	if err != nil {
		fail(err)
		return
	}
	s.limiter.succeed(ip)
	s.startSession(w, credential.ID)
	log.Printf("auth: passkey sign-in from %s", ip)
	writeJSON(w, http.StatusOK, map[string]any{"authenticated": true})
}

// handleRegisterBegin starts a registration. It needs a valid session or the
// one-time setup code from `nubererum passkey-setup`.
func (s *passkeyServer) handleRegisterBegin(w http.ResponseWriter, r *http.Request) {
	ip := clientIP(r)
	if s.rejectBlocked(w, ip) {
		return
	}
	origin, party, ok := s.relyingParty(r)
	if !ok {
		writeUnknownOrigin(w)
		return
	}
	var body struct {
		SetupCode string `json:"setupCode"`
		Name      string `json:"name"`
	}
	if err := readJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Invalid request."})
		return
	}
	setupCode := ""
	if !s.authenticated(r) {
		if !setupCodeValid(s.dir, body.SetupCode, s.now()) {
			s.recordFailure(ip, "setup code", errors.New("wrong or expired setup code"))
			writeJSON(w, http.StatusForbidden, map[string]any{"error": "The setup code is wrong or expired."})
			return
		}
		setupCode = normalizeSetupCode(body.SetupCode)
	}
	data, err := s.store.ensureUserHandle()
	if err != nil {
		log.Printf("auth: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "Passkey storage is not writable."})
		return
	}
	user := passkeyUser{data: data}
	exclusions := make([]protocol.CredentialDescriptor, 0, len(data.Passkeys))
	for _, passkey := range data.Passkeys {
		if s.rpidFor(passkey) == party.Config.RPID {
			exclusions = append(exclusions, passkey.Credential.Descriptor())
		}
	}
	options, session, err := party.BeginRegistration(user,
		webauthn.WithResidentKeyRequirement(protocol.ResidentKeyRequirementRequired),
		webauthn.WithExclusions(exclusions),
	)
	if err != nil {
		log.Printf("auth: begin registration: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "Could not start registration."})
		return
	}
	id := s.addCeremony(passkeyCeremony{
		origin:    origin,
		register:  true,
		data:      *session,
		setupCode: setupCode,
		name:      cleanPasskeyName(body.Name, s.now()),
	})
	writeJSON(w, http.StatusOK, map[string]any{"ceremony": id, "options": options})
}

func (s *passkeyServer) handleRegisterFinish(w http.ResponseWriter, r *http.Request) {
	ip := clientIP(r)
	if s.rejectBlocked(w, ip) {
		return
	}
	reject := func(status int, message string, err error) {
		s.recordFailure(ip, "passkey registration", err)
		writeJSON(w, status, map[string]any{"error": message})
	}
	body, err := readCeremonyResponse(r)
	if err != nil {
		reject(http.StatusBadRequest, "Invalid request.", err)
		return
	}
	ceremony, ok := s.takeCeremony(body.Ceremony, true)
	if !ok {
		reject(http.StatusBadRequest, "The registration expired. Start again.", errors.New("unknown or expired ceremony"))
		return
	}
	party := s.parties[ceremony.origin]
	if requestOrigin(r) != ceremony.origin || party == nil {
		reject(http.StatusForbidden, "Finish the registration on the same address.", errors.New("ceremony finished from another origin"))
		return
	}
	// A ceremony started with a session needs that session to finish it.
	if ceremony.setupCode == "" && !s.authenticated(r) {
		reject(http.StatusUnauthorized, "Sign in again to add a passkey.", errors.New("session ended during registration"))
		return
	}
	parsed, err := protocol.ParseCredentialCreationResponseBytes(body.Credential)
	if err != nil {
		reject(http.StatusBadRequest, "The passkey response was invalid.", err)
		return
	}
	data, err := s.store.load()
	if err != nil {
		log.Printf("auth: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "Passkey storage is unreadable."})
		return
	}
	credential, err := party.CreateCredential(passkeyUser{data: data}, ceremony.data, parsed)
	if err != nil {
		reject(http.StatusBadRequest, "The passkey could not be verified.", err)
		return
	}
	if ceremony.setupCode != "" && !consumeSetupCode(s.dir, ceremony.setupCode, s.now()) {
		reject(http.StatusForbidden, "The setup code is wrong or expired.", errors.New("setup code used or expired before registration finished"))
		return
	}
	stored := storedPasskey{Name: ceremony.name, RPID: party.Config.RPID, CreatedAt: s.now().UTC(), Credential: *credential}
	err = s.store.update(func(current *passkeyData) error {
		if subtle.ConstantTimeCompare(current.UserHandle, ceremony.data.UserID) != 1 {
			return errors.New("user handle changed during registration")
		}
		if current.find(credential.ID) >= 0 {
			return errors.New("passkey is already registered")
		}
		current.Passkeys = append(current.Passkeys, stored)
		return nil
	})
	if err != nil {
		log.Printf("auth: store passkey: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "The passkey could not be saved."})
		return
	}
	s.limiter.succeed(ip)
	if ceremony.setupCode != "" {
		s.startSession(w, credential.ID)
	}
	log.Printf("auth: registered passkey %q (%s) for %s from %s", stored.Name, stored.ID(), stored.RPID, ip)
	writeJSON(w, http.StatusOK, map[string]any{"registered": true, "name": stored.Name, "id": stored.ID()})
}

func (s *passkeyServer) handleLogout(w http.ResponseWriter, r *http.Request) {
	if key, _, ok := s.session(r); ok {
		s.mu.Lock()
		delete(s.sessions, key)
		s.mu.Unlock()
	}
	clearSessionCookie(w)
	writeJSON(w, http.StatusOK, map[string]any{"authenticated": false})
}

// CLI subcommands, run inside the container:
//   nubererum passkey-setup | passkey-list | passkey-remove <id>

func runPasskeyCommand(args []string, stdout, stderr io.Writer) int {
	dir := passkeyDataDir()
	store := newPasskeyStore(dir)
	switch args[0] {
	case "passkey-setup":
		code, expires, err := createSetupCode(dir, time.Now())
		if err != nil {
			fmt.Fprintf(stderr, "passkey-setup: %v\n", err)
			return 1
		}
		fmt.Fprintf(stdout, "Setup code: %s\nValid once until %s. Enter it on /login to register a passkey.\n", code, expires.Local().Format("2006-01-02 15:04 MST"))
		return 0
	case "passkey-list":
		data, err := store.load()
		if err != nil {
			fmt.Fprintf(stderr, "passkey-list: %v\n", err)
			return 1
		}
		if len(data.Passkeys) == 0 {
			fmt.Fprintln(stdout, "No passkeys registered.")
			return 0
		}
		passkeys := append([]storedPasskey(nil), data.Passkeys...)
		sort.SliceStable(passkeys, func(i, j int) bool { return passkeys[i].CreatedAt.Before(passkeys[j].CreatedAt) })
		for _, passkey := range passkeys {
			lastUsed := "never"
			if !passkey.LastUsedAt.IsZero() {
				lastUsed = passkey.LastUsedAt.Local().Format("2006-01-02 15:04")
			}
			rpid := passkey.RPID
			if rpid == "" {
				rpid = "(first origin)"
			}
			fmt.Fprintf(stdout, "%s  %q  %s  created %s  last used %s\n", passkey.ID(), passkey.Name, rpid, passkey.CreatedAt.Local().Format("2006-01-02 15:04"), lastUsed)
		}
		return 0
	case "passkey-remove":
		if len(args) != 2 || strings.TrimSpace(args[1]) == "" {
			fmt.Fprintln(stderr, "usage: nubererum passkey-remove <id>")
			return 2
		}
		removed, err := store.removePasskey(strings.TrimSpace(args[1]))
		if err != nil {
			fmt.Fprintf(stderr, "passkey-remove: %v\n", err)
			return 1
		}
		fmt.Fprintf(stdout, "Removed passkey %q (%s). Its sessions end on their next request.\n", removed.Name, removed.ID())
		return 0
	}
	fmt.Fprintf(stderr, "unknown command %q (use passkey-setup, passkey-list, passkey-remove <id>, mcp-token-create, mcp-token-list, mcp-token-remove <name> or mcp-token-projects)\n", args[0])
	return 2
}
