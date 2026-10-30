package main

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/go-webauthn/webauthn/protocol/webauthncbor"
)

const testPasskeyOrigin = "https://things.example.test"

var b64url = base64.RawURLEncoding

// softAuthenticator is a minimal WebAuthn authenticator with one P-256
// discoverable credential and "none" attestation.
type softAuthenticator struct {
	t          *testing.T
	origin     string
	key        *ecdsa.PrivateKey
	id         []byte
	userHandle []byte
	signCount  uint32
	flags      byte
}

const (
	flagUP = 0x01
	flagUV = 0x04
	flagAT = 0x40
)

func newSoftAuthenticator(t *testing.T) *softAuthenticator {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return &softAuthenticator{t: t, origin: testPasskeyOrigin, key: key, id: randomBytes(16), flags: flagUP | flagUV}
}

type beginResponse struct {
	Ceremony string `json:"ceremony"`
	Options  struct {
		PublicKey struct {
			Challenge string `json:"challenge"`
			RP        struct {
				ID string `json:"id"`
			} `json:"rp"`
			RPID string `json:"rpId"`
			User struct {
				ID string `json:"id"`
			} `json:"user"`
			UserVerification       string `json:"userVerification"`
			AuthenticatorSelection struct {
				ResidentKey      string `json:"residentKey"`
				UserVerification string `json:"userVerification"`
			} `json:"authenticatorSelection"`
		} `json:"publicKey"`
	} `json:"options"`
}

func (a *softAuthenticator) clientData(kind, challenge string) []byte {
	data, _ := json.Marshal(map[string]any{"type": kind, "challenge": challenge, "origin": a.origin, "crossOrigin": false})
	return data
}

func (a *softAuthenticator) authData(rpID string, flags byte, attested []byte) []byte {
	rpHash := sha256.Sum256([]byte(rpID))
	out := append([]byte{}, rpHash[:]...)
	out = append(out, flags)
	out = binary.BigEndian.AppendUint32(out, a.signCount)
	return append(out, attested...)
}

func (a *softAuthenticator) register(begin beginResponse) []byte {
	options := begin.Options.PublicKey
	userHandle, err := b64url.DecodeString(options.User.ID)
	if err != nil {
		a.t.Fatal(err)
	}
	a.userHandle = userHandle
	publicKey := a.key.PublicKey
	x, y := make([]byte, 32), make([]byte, 32)
	publicKey.X.FillBytes(x)
	publicKey.Y.FillBytes(y)
	cose, err := webauthncbor.Marshal(map[int]any{1: 2, 3: -7, -1: 1, -2: x, -3: y})
	if err != nil {
		a.t.Fatal(err)
	}
	attested := make([]byte, 16) // zero AAGUID
	attested = binary.BigEndian.AppendUint16(attested, uint16(len(a.id)))
	attested = append(attested, a.id...)
	attested = append(attested, cose...)
	attestation, err := webauthncbor.Marshal(map[string]any{
		"fmt":      "none",
		"attStmt":  map[string]any{},
		"authData": a.authData(options.RP.ID, a.flags|flagAT, attested),
	})
	if err != nil {
		a.t.Fatal(err)
	}
	return a.envelope(map[string]any{
		"clientDataJSON":    b64url.EncodeToString(a.clientData("webauthn.create", options.Challenge)),
		"attestationObject": b64url.EncodeToString(attestation),
		"transports":        []string{"internal"},
	})
}

func (a *softAuthenticator) assert(begin beginResponse) []byte {
	options := begin.Options.PublicKey
	a.signCount++
	authData := a.authData(options.RPID, a.flags, nil)
	clientData := a.clientData("webauthn.get", options.Challenge)
	clientHash := sha256.Sum256(clientData)
	digest := sha256.Sum256(append(append([]byte{}, authData...), clientHash[:]...))
	signature, err := ecdsa.SignASN1(rand.Reader, a.key, digest[:])
	if err != nil {
		a.t.Fatal(err)
	}
	return a.envelope(map[string]any{
		"clientDataJSON":    b64url.EncodeToString(clientData),
		"authenticatorData": b64url.EncodeToString(authData),
		"signature":         b64url.EncodeToString(signature),
		"userHandle":        b64url.EncodeToString(a.userHandle),
	})
}

func (a *softAuthenticator) envelope(response map[string]any) []byte {
	data, _ := json.Marshal(map[string]any{
		"id":                     b64url.EncodeToString(a.id),
		"rawId":                  b64url.EncodeToString(a.id),
		"type":                   "public-key",
		"response":               response,
		"clientExtensionResults": map[string]any{},
	})
	return data
}

type passkeyHarness struct {
	t       *testing.T
	origin  string
	dir     string
	server  *passkeyServer
	handler http.Handler
}

func newPasskeyHarness(t *testing.T) *passkeyHarness {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("APP_AUTH", "passkey")
	t.Setenv("APP_PASSKEY_ORIGIN", testPasskeyOrigin)
	t.Setenv("APP_PUBLIC_ORIGINS", "")
	t.Setenv("DATA_DIR", dir)
	server, err := newPasskeyServer(dir, newAuthLimiter())
	if err != nil {
		t.Fatal(err)
	}
	protected := http.NewServeMux()
	protected.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	return &passkeyHarness{t: t, origin: testPasskeyOrigin, dir: dir, server: server, handler: sameOriginGuard(server.middleware(protected))}
}

func (h *passkeyHarness) request(method, path string, body any, cookie *http.Cookie) *httptest.ResponseRecorder {
	h.t.Helper()
	var reader *bytes.Reader
	switch value := body.(type) {
	case nil:
		reader = bytes.NewReader(nil)
	case []byte:
		reader = bytes.NewReader(value)
	default:
		data, err := json.Marshal(value)
		if err != nil {
			h.t.Fatal(err)
		}
		reader = bytes.NewReader(data)
	}
	request := httptest.NewRequest(method, h.origin+path, reader)
	request.Header.Set("X-Forwarded-For", "203.0.113.7")
	if method == http.MethodPost {
		request.Header.Set("Origin", h.origin)
		request.Header.Set("Content-Type", "application/json")
	}
	if cookie != nil {
		request.AddCookie(cookie)
	}
	recorder := httptest.NewRecorder()
	h.handler.ServeHTTP(recorder, request)
	return recorder
}

func (h *passkeyHarness) begin(path string, body any, cookie *http.Cookie) beginResponse {
	h.t.Helper()
	response := h.request(http.MethodPost, path, body, cookie)
	if response.Code != http.StatusOK {
		h.t.Fatalf("%s code = %d, body %s", path, response.Code, response.Body)
	}
	var begin beginResponse
	if err := json.Unmarshal(response.Body.Bytes(), &begin); err != nil {
		h.t.Fatal(err)
	}
	return begin
}

func (h *passkeyHarness) finish(path, ceremony string, credential []byte, cookie *http.Cookie) *httptest.ResponseRecorder {
	h.t.Helper()
	return h.request(http.MethodPost, path, map[string]any{"ceremony": ceremony, "credential": json.RawMessage(credential)}, cookie)
}

func (h *passkeyHarness) setupCode() string {
	h.t.Helper()
	code, _, err := createSetupCode(h.dir, time.Now())
	if err != nil {
		h.t.Fatal(err)
	}
	return code
}

// registerWithCode registers authenticator with a fresh setup code and
// returns the session cookie.
func (h *passkeyHarness) registerWithCode(authenticator *softAuthenticator, name string) *http.Cookie {
	h.t.Helper()
	begin := h.begin("/auth/register/begin", map[string]string{"setupCode": h.setupCode(), "name": name}, nil)
	response := h.finish("/auth/register/finish", begin.Ceremony, authenticator.register(begin), nil)
	if response.Code != http.StatusOK {
		h.t.Fatalf("register finish code = %d, body %s", response.Code, response.Body)
	}
	return sessionCookie(h.t, response)
}

func (h *passkeyHarness) login(authenticator *softAuthenticator) *httptest.ResponseRecorder {
	h.t.Helper()
	begin := h.begin("/auth/login/begin", nil, nil)
	return h.finish("/auth/login/finish", begin.Ceremony, authenticator.assert(begin), nil)
}

func sessionCookie(t *testing.T, response *httptest.ResponseRecorder) *http.Cookie {
	t.Helper()
	for _, cookie := range response.Result().Cookies() {
		if cookie.Name == passkeyCookieName && cookie.Value != "" {
			return cookie
		}
	}
	t.Fatalf("no %s cookie in response", passkeyCookieName)
	return nil
}

func TestValidateSecurityConfigPasskeyMode(t *testing.T) {
	t.Setenv("APP_PASSWORD", "")
	t.Setenv("APP_AUTH", "passkey")
	t.Setenv("APP_PASSKEY_ORIGIN", "")
	t.Setenv("APP_PUBLIC_ORIGINS", "")
	if err := validateSecurityConfig("0.0.0.0"); err == nil {
		t.Fatal("expected passkey mode without origin to be rejected")
	}
	t.Setenv("APP_PUBLIC_ORIGINS", "http://things.example.test")
	if err := validateSecurityConfig("0.0.0.0"); err == nil {
		t.Fatal("expected plain-http origin to be rejected")
	}
	t.Setenv("APP_PUBLIC_ORIGINS", "https://things.example.test/app")
	if err := validateSecurityConfig("0.0.0.0"); err == nil {
		t.Fatal("expected origin with path to be rejected")
	}
	for _, origin := range []string{"https://things.example.test", "http://localhost:3007"} {
		t.Setenv("APP_PUBLIC_ORIGINS", origin+", https://other.example.test")
		if err := validateSecurityConfig("0.0.0.0"); err != nil {
			t.Fatalf("origin %s rejected: %v", origin, err)
		}
	}
	t.Setenv("APP_AUTH", "magic")
	if err := validateSecurityConfig("127.0.0.1"); err == nil {
		t.Fatal("expected unknown APP_AUTH to be rejected")
	}
}

func TestPasskeyModeRequiresSession(t *testing.T) {
	h := newPasskeyHarness(t)

	response := h.request(http.MethodGet, "/api/sync", nil, nil)
	if response.Code != http.StatusUnauthorized || !strings.Contains(response.Header().Get("Content-Type"), "application/json") {
		t.Fatalf("/api/sync without session = %d %q, want JSON 401", response.Code, response.Header().Get("Content-Type"))
	}
	response = h.request(http.MethodGet, "/", nil, nil)
	if response.Code != http.StatusSeeOther || response.Header().Get("Location") != "/login" {
		t.Fatalf("/ without session = %d %q, want redirect to /login", response.Code, response.Header().Get("Location"))
	}
	if response = h.request(http.MethodPost, "/api/create", map[string]string{}, nil); response.Code != http.StatusUnauthorized {
		t.Fatalf("POST /api/create without session = %d, want 401", response.Code)
	}
	if response = h.request(http.MethodGet, "/login.js", nil, nil); response.Code != http.StatusNoContent {
		t.Fatalf("/login.js without session = %d, want it served", response.Code)
	}
	forged := &http.Cookie{Name: passkeyCookieName, Value: randomToken()}
	if response = h.request(http.MethodGet, "/api/sync", nil, forged); response.Code != http.StatusUnauthorized {
		t.Fatalf("/api/sync with unknown token = %d, want 401", response.Code)
	}

	response = h.request(http.MethodGet, "/auth/status", nil, nil)
	var status map[string]any
	_ = json.Unmarshal(response.Body.Bytes(), &status)
	if status["authenticated"] != false || status["registered"] != false || status["mode"] != "passkey" {
		t.Fatalf("status = %v", status)
	}
}

func TestPasskeyRegistrationWithSetupCodeAndLogin(t *testing.T) {
	h := newPasskeyHarness(t)
	authenticator := newSoftAuthenticator(t)

	code := h.setupCode()
	if response := h.request(http.MethodPost, "/auth/register/begin", map[string]string{"setupCode": "AAAA-BBBB-CCCC-DDDD"}, nil); response.Code != http.StatusForbidden {
		t.Fatalf("wrong setup code = %d, want 403", response.Code)
	}
	begin := h.begin("/auth/register/begin", map[string]string{"setupCode": strings.ToLower(code), "name": "  Alex's iPhone\n"}, nil)
	options := begin.Options.PublicKey
	if options.RP.ID != "things.example.test" || options.AuthenticatorSelection.ResidentKey != "required" || options.AuthenticatorSelection.UserVerification != "required" {
		t.Fatalf("creation options = %+v", options)
	}
	response := h.finish("/auth/register/finish", begin.Ceremony, authenticator.register(begin), nil)
	if response.Code != http.StatusOK {
		t.Fatalf("register finish = %d %s", response.Code, response.Body)
	}
	cookie := sessionCookie(t, response)
	if !cookie.Secure || !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode || cookie.Path != "/" || len(cookie.Value) < 40 {
		t.Fatalf("session cookie = %+v", cookie)
	}
	if response := h.request(http.MethodGet, "/api/sync", nil, cookie); response.Code != http.StatusNoContent {
		t.Fatalf("/api/sync with session = %d, want 204", response.Code)
	}

	// The setup code works once.
	if response := h.request(http.MethodPost, "/auth/register/begin", map[string]string{"setupCode": code}, nil); response.Code != http.StatusForbidden {
		t.Fatalf("reused setup code = %d, want 403", response.Code)
	}

	data, err := h.server.store.load()
	if err != nil || len(data.Passkeys) != 1 || data.Passkeys[0].Name != "Alex's iPhone" || len(data.UserHandle) != passkeyUserHandleSize {
		t.Fatalf("stored passkeys = %+v, err %v", data, err)
	}

	if response := h.request(http.MethodPost, "/auth/logout", map[string]string{}, cookie); response.Code != http.StatusOK {
		t.Fatalf("logout = %d", response.Code)
	}
	if response := h.request(http.MethodGet, "/api/sync", nil, cookie); response.Code != http.StatusUnauthorized {
		t.Fatalf("/api/sync after logout = %d, want 401", response.Code)
	}

	response = h.login(authenticator)
	if response.Code != http.StatusOK {
		t.Fatalf("login finish = %d %s", response.Code, response.Body)
	}
	cookie = sessionCookie(t, response)
	if response := h.request(http.MethodGet, "/api/sync", nil, cookie); response.Code != http.StatusNoContent {
		t.Fatalf("/api/sync after login = %d, want 204", response.Code)
	}
	data, _ = h.server.store.load()
	if data.Passkeys[0].Credential.Authenticator.SignCount != 1 || data.Passkeys[0].LastUsedAt.IsZero() {
		t.Fatalf("login did not update the stored credential: %+v", data.Passkeys[0])
	}

	response = h.request(http.MethodGet, "/auth/status", nil, cookie)
	var status map[string]any
	_ = json.Unmarshal(response.Body.Bytes(), &status)
	if status["authenticated"] != true || status["registered"] != true || status["passkey"] != "Alex's iPhone" {
		t.Fatalf("status = %v", status)
	}
}

func TestPasskeyRegistrationNeedsSessionOrCode(t *testing.T) {
	h := newPasskeyHarness(t)
	first := newSoftAuthenticator(t)
	cookie := h.registerWithCode(first, "first")

	if response := h.request(http.MethodPost, "/auth/register/begin", map[string]string{}, nil); response.Code != http.StatusForbidden {
		t.Fatalf("register without session or code = %d, want 403", response.Code)
	}

	second := newSoftAuthenticator(t)
	begin := h.begin("/auth/register/begin", map[string]string{"name": "second"}, cookie)
	// A session-started registration cannot be finished without the session.
	if response := h.finish("/auth/register/finish", begin.Ceremony, second.register(begin), nil); response.Code != http.StatusUnauthorized {
		t.Fatalf("finish without session = %d, want 401", response.Code)
	}
	begin = h.begin("/auth/register/begin", map[string]string{"name": "second"}, cookie)
	response := h.finish("/auth/register/finish", begin.Ceremony, second.register(begin), cookie)
	if response.Code != http.StatusOK {
		t.Fatalf("finish with session = %d %s", response.Code, response.Body)
	}
	if response := h.login(second); response.Code != http.StatusOK {
		t.Fatalf("login with second passkey = %d %s", response.Code, response.Body)
	}
	data, _ := h.server.store.load()
	if len(data.Passkeys) != 2 {
		t.Fatalf("passkeys = %d, want 2", len(data.Passkeys))
	}
}

func TestPasskeyRegistrationRequiresUserVerification(t *testing.T) {
	h := newPasskeyHarness(t)
	authenticator := newSoftAuthenticator(t)
	authenticator.flags = flagUP
	begin := h.begin("/auth/register/begin", map[string]string{"setupCode": h.setupCode()}, nil)
	if response := h.finish("/auth/register/finish", begin.Ceremony, authenticator.register(begin), nil); response.Code != http.StatusBadRequest {
		t.Fatalf("registration without UV = %d, want 400", response.Code)
	}
}

func TestPasskeyLoginRejectsBadAssertions(t *testing.T) {
	h := newPasskeyHarness(t)
	authenticator := newSoftAuthenticator(t)
	h.registerWithCode(authenticator, "phone")

	// Without user verification.
	authenticator.flags = flagUP
	if response := h.login(authenticator); response.Code != http.StatusUnauthorized {
		t.Fatalf("login without UV = %d, want 401", response.Code)
	}
	authenticator.flags = flagUP | flagUV

	// A ceremony can be finished only once.
	begin := h.begin("/auth/login/begin", nil, nil)
	credential := authenticator.assert(begin)
	if response := h.finish("/auth/login/finish", begin.Ceremony, credential, nil); response.Code != http.StatusOK {
		t.Fatalf("login = %d %s", response.Code, response.Body)
	}
	if response := h.finish("/auth/login/finish", begin.Ceremony, credential, nil); response.Code != http.StatusUnauthorized {
		t.Fatalf("replayed ceremony = %d, want 401", response.Code)
	}

	// A different key with the same credential ID.
	impostor := newSoftAuthenticator(t)
	impostor.id, impostor.userHandle = authenticator.id, authenticator.userHandle
	if response := h.login(impostor); response.Code != http.StatusUnauthorized {
		t.Fatalf("login with wrong key = %d, want 401", response.Code)
	}

	// A counter that goes backwards signals a cloned authenticator.
	authenticator.signCount = 0
	if response := h.login(authenticator); response.Code != http.StatusUnauthorized {
		t.Fatalf("login with stale counter = %d, want 401", response.Code)
	}
}

func TestPasskeyFailuresAreRateLimited(t *testing.T) {
	h := newPasskeyHarness(t)
	for i := 0; i < authFailuresPerIP; i++ {
		if response := h.request(http.MethodPost, "/auth/register/begin", map[string]string{"setupCode": "wrong"}, nil); response.Code != http.StatusForbidden {
			t.Fatalf("wrong code %d = %d, want 403", i+1, response.Code)
		}
	}
	code := h.setupCode()
	if response := h.request(http.MethodPost, "/auth/register/begin", map[string]string{"setupCode": code}, nil); response.Code != http.StatusTooManyRequests {
		t.Fatalf("blocked client with valid code = %d, want 429", response.Code)
	}
	if response := h.request(http.MethodPost, "/auth/login/begin", nil, nil); response.Code != http.StatusTooManyRequests {
		t.Fatalf("blocked client login = %d, want 429", response.Code)
	}
}

func TestPasskeyRemovalEndsSessions(t *testing.T) {
	h := newPasskeyHarness(t)
	authenticator := newSoftAuthenticator(t)
	cookie := h.registerWithCode(authenticator, "laptop")

	var out, errOut bytes.Buffer
	if code := runPasskeyCommand([]string{"passkey-list"}, &out, &errOut); code != 0 || !strings.Contains(out.String(), `"laptop"`) {
		t.Fatalf("passkey-list = %d %q %q", code, out.String(), errOut.String())
	}
	id := b64url.EncodeToString(authenticator.id)
	out.Reset()
	if code := runPasskeyCommand([]string{"passkey-remove", id[:8]}, &out, &errOut); code != 0 {
		t.Fatalf("passkey-remove = %d %q", code, errOut.String())
	}
	if response := h.request(http.MethodGet, "/api/sync", nil, cookie); response.Code != http.StatusUnauthorized {
		t.Fatalf("session of removed passkey = %d, want 401", response.Code)
	}
	if response := h.login(authenticator); response.Code != http.StatusUnauthorized {
		t.Fatalf("login with removed passkey = %d, want 401", response.Code)
	}
	if code := runPasskeyCommand([]string{"passkey-remove", id}, &out, &errOut); code != 1 {
		t.Fatalf("removing a missing passkey = %d, want 1", code)
	}
}

func TestPasskeySetupCodeCommandAndExpiry(t *testing.T) {
	h := newPasskeyHarness(t)
	var out, errOut bytes.Buffer
	if code := runPasskeyCommand([]string{"passkey-setup"}, &out, &errOut); code != 0 {
		t.Fatalf("passkey-setup = %d %q", code, errOut.String())
	}
	line := strings.SplitN(out.String(), "\n", 2)[0]
	code := strings.TrimPrefix(line, "Setup code: ")
	if len(code) != 19 || !setupCodeValid(h.dir, code, time.Now()) {
		t.Fatalf("setup code %q not valid", code)
	}
	if setupCodeValid(h.dir, code, time.Now().Add(passkeySetupCodeTTL+time.Second)) {
		t.Fatal("expired setup code accepted")
	}
	stored, _ := readFileString(h.dir + "/" + passkeySetupFileName)
	if strings.Contains(stored, normalizeSetupCode(code)) {
		t.Fatal("setup code stored in plain text")
	}
}

func TestPasskeySessionExpires(t *testing.T) {
	h := newPasskeyHarness(t)
	cookie := h.registerWithCode(newSoftAuthenticator(t), "phone")
	h.server.now = func() time.Time { return time.Now().Add(passkeySessionTTL + time.Minute) }
	if response := h.request(http.MethodGet, "/api/sync", nil, cookie); response.Code != http.StatusUnauthorized {
		t.Fatalf("expired session = %d, want 401", response.Code)
	}
}

func TestPasskeyAuthRejectsCrossOriginPosts(t *testing.T) {
	h := newPasskeyHarness(t)
	request := httptest.NewRequest(http.MethodPost, testPasskeyOrigin+"/auth/login/begin", strings.NewReader("{}"))
	request.Header.Set("Origin", "https://evil.example")
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	h.handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("cross-origin login begin = %d, want 403", recorder.Code)
	}
}

func readFileString(path string) (string, error) {
	data, err := os.ReadFile(path)
	return string(data), err
}

func TestPasskeysAreBoundToTheirOrigin(t *testing.T) {
	const lanOrigin = "https://things.lan.example"
	t.Setenv("APP_PASSKEY_ORIGIN", "")
	t.Setenv("APP_PUBLIC_ORIGINS", testPasskeyOrigin+","+lanOrigin)
	dir := t.TempDir()
	server, err := newPasskeyServer(dir, newAuthLimiter())
	if err != nil {
		t.Fatal(err)
	}
	protected := http.NewServeMux()
	protected.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) })
	public := &passkeyHarness{t: t, origin: testPasskeyOrigin, dir: dir, server: server, handler: sameOriginGuard(server.middleware(protected))}
	lan := &passkeyHarness{t: t, origin: lanOrigin, dir: dir, server: server, handler: public.handler}

	phone := newSoftAuthenticator(t)
	public.registerWithCode(phone, "phone")

	// The LAN origin is its own relying party: the public passkey does not work there.
	begin := lan.begin("/auth/login/begin", nil, nil)
	if begin.Options.PublicKey.RPID != "things.lan.example" {
		t.Fatalf("LAN rpId = %q", begin.Options.PublicKey.RPID)
	}
	phone.origin = lanOrigin
	if response := lan.finish("/auth/login/finish", begin.Ceremony, phone.assert(begin), nil); response.Code != http.StatusUnauthorized {
		t.Fatalf("public passkey on LAN origin = %d, want 401", response.Code)
	}
	status := lan.request(http.MethodGet, "/auth/status", nil, nil)
	if !strings.Contains(status.Body.String(), `"registered":false`) {
		t.Fatalf("LAN status = %s, want no passkey registered there", status.Body)
	}

	// A ceremony cannot be finished on another origin.
	begin = public.begin("/auth/login/begin", nil, nil)
	if response := lan.finish("/auth/login/finish", begin.Ceremony, phone.assert(begin), nil); response.Code != http.StatusUnauthorized {
		t.Fatalf("cross-origin finish = %d, want 401", response.Code)
	}

	laptop := newSoftAuthenticator(t)
	laptop.origin = lanOrigin
	cookie := lan.registerWithCode(laptop, "laptop")
	if response := lan.request(http.MethodGet, "/api/sync", nil, cookie); response.Code != http.StatusNoContent {
		t.Fatalf("LAN session = %d, want 204", response.Code)
	}
	if response := lan.login(laptop); response.Code != http.StatusOK {
		t.Fatalf("LAN login = %d %s", response.Code, response.Body)
	}
	data, _ := server.store.load()
	if len(data.Passkeys) != 2 || data.Passkeys[0].RPID != "things.example.test" || data.Passkeys[1].RPID != "things.lan.example" {
		t.Fatalf("stored passkeys = %+v", data.Passkeys)
	}

	// Passkeys stored before multi-origin support belong to the first origin.
	if err := server.store.update(func(d *passkeyData) error { d.Passkeys[0].RPID = ""; return nil }); err != nil {
		t.Fatal(err)
	}
	phone.origin = testPasskeyOrigin
	if response := public.login(phone); response.Code != http.StatusOK {
		t.Fatalf("legacy passkey on first origin = %d %s", response.Code, response.Body)
	}

	// Origins that are not configured get no ceremony.
	other := &passkeyHarness{t: t, origin: "https://elsewhere.example", dir: dir, server: server, handler: server.middleware(protected)}
	if response := other.request(http.MethodPost, "/auth/login/begin", nil, nil); response.Code != http.StatusForbidden {
		t.Fatalf("unknown origin login begin = %d, want 403", response.Code)
	}
}
