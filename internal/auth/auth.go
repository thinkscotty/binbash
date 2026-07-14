// Package auth owns binbash's HTTP trust boundary: single-password login,
// signed session cookies, in-app password rotation, brute-force throttling,
// and the per-request protections (CSRF, security headers) wrapped around
// every route by Protect.
package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// MaxAttempts is how many failed password attempts an IP gets, within
// ThrottleWindow, before it is locked out for LockoutDuration. Exported so the
// auth-failure log can name the number an operator will see in their fail2ban
// config.
const MaxAttempts = 5

// Password length limits, enforced everywhere a password can be set: the
// bootstrap value in the config file, the in-app change form, and the
// -reset-password command. They live here, in the package that owns passwords,
// because three copies of "8" and "72" is three chances for one of them to
// drift and let someone set a password that another path then rejects.
//
// MaxPasswordLen is bcrypt's hard limit, in bytes: GenerateFromPassword errors
// out above it rather than truncating, so without the check a too-long password
// surfaces as a bcrypt error from deep in the auth internals, with nothing
// telling the operator that length was the problem.
const (
	MinPasswordLen = 8
	MaxPasswordLen = 72
)

// ValidatePassword reports a human-readable problem with a proposed password,
// or "" if it's acceptable.
func ValidatePassword(password string) string {
	switch {
	case password == "":
		return "Password is required"
	case len(password) < MinPasswordLen:
		return fmt.Sprintf("Password must be at least %d characters", MinPasswordLen)
	case len(password) > MaxPasswordLen:
		return fmt.Sprintf("Password is too long (max %d characters)", MaxPasswordLen)
	}
	return ""
}

const (
	cookieName = "binbash_session"
	sessionTTL = 30 * 24 * time.Hour

	throttleWindow  = 15 * time.Minute
	lockoutDuration = 15 * time.Minute
	throttleIdleTTL = time.Hour // sweep records idle longer than this
)

// Auth checks passwords, issues/validates signed session cookies, and
// throttles repeated failed login attempts. The password hash and session-
// signing key are persisted in the database (auth_settings table) so the
// password can be rotated in-app without a restart, and cached in memory
// (guarded by mu) so the hot paths -- IsAuthenticated on every request,
// CheckPassword on login -- never need a DB round trip.
//
// Rotating the password also rotates the session-signing key, which
// invalidates every session signed with the old one. That is what makes
// "change the password" a working panic button: a session cookie that leaked
// is otherwise valid for its full 30-day life, and no amount of password
// changing would take it away. Rotate re-issues the caller's own cookie in the
// same response, so the person changing the password stays signed in while
// every other device has to sign in again.
type Auth struct {
	db      *sql.DB
	proxies *TrustedProxies
	csrf    *http.CrossOriginProtection

	mu           sync.RWMutex
	passwordHash []byte
	key          []byte

	throttleMu sync.Mutex
	attempts   map[string]*attempt
}

type attempt struct {
	count       int
	windowStart time.Time
	lockedUntil time.Time
	lastSeen    time.Time
}

// New loads the persisted password hash and session key from auth_settings,
// bootstrapping that row from bootstrapPassword on first run (a fresh
// database, or one upgrading from before password rotation existed). On
// every later run the database is the source of truth; bootstrapPassword is
// ignored once a row exists.
//
// That "ignored once a row exists" is why bootstrapPassword is allowed to be
// empty. It is needed exactly once, to create the account; after that, keeping
// it in the config file leaves a plaintext secret on disk that unlocks nothing.
// So an empty value is only an error when there is no account to fall back on,
// and when one is supplied redundantly we say so, because an operator can't be
// expected to guess that the password in their config stopped mattering.
//
// proxies decides whose X-Forwarded-* headers are believed; pass
// DefaultTrustedProxies() for the loopback-only default.
func New(db *sql.DB, bootstrapPassword string, proxies *TrustedProxies) (*Auth, error) {
	hash, key, err := loadSettings(db)
	if err != nil {
		return nil, fmt.Errorf("load auth settings: %w", err)
	}

	switch {
	case hash == nil && bootstrapPassword == "":
		return nil, errors.New("no password is set, and binbash has no account yet. " +
			"Set one in binbash.toml (password = \"...\") or via BINBASH_PASSWORD, then start binbash again. " +
			"You only need it this once: after you sign in you can change the password in Settings, and remove it from the config file")

	case hash == nil:
		hash, key, err = bootstrap(db, bootstrapPassword)
		if err != nil {
			return nil, fmt.Errorf("bootstrap auth settings: %w", err)
		}
		log.Printf("created your binbash account from the configured password")

	case bootstrapPassword != "":
		log.Printf("note: binbash already has an account, so the password in your config is not used — the one you set in the app is. You can delete it from the config file (if you ever forget your password, run binbash -reset-password)")
	}

	if proxies == nil {
		proxies = DefaultTrustedProxies()
	}

	return &Auth{
		db:           db,
		proxies:      proxies,
		csrf:         http.NewCrossOriginProtection(),
		passwordHash: hash,
		key:          key,
		attempts:     make(map[string]*attempt),
	}, nil
}

func loadSettings(db *sql.DB) (hash, key []byte, err error) {
	var hashStr, keyB64 string
	err = db.QueryRow(`SELECT password_hash, session_key FROM auth_settings WHERE id = 1`).Scan(&hashStr, &keyB64)
	if err == sql.ErrNoRows {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	key, err = base64.StdEncoding.DecodeString(keyB64)
	if err != nil {
		return nil, nil, fmt.Errorf("decode session key: %w", err)
	}
	return []byte(hashStr), key, nil
}

func bootstrap(db *sql.DB, password string) (hash, key []byte, err error) {
	hash, err = bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, nil, fmt.Errorf("hash bootstrap password: %w", err)
	}
	key = make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, nil, fmt.Errorf("generate session key: %w", err)
	}

	if _, err := db.Exec(
		`INSERT INTO auth_settings (id, password_hash, session_key) VALUES (1, ?, ?)`,
		string(hash), base64.StdEncoding.EncodeToString(key),
	); err != nil {
		return nil, nil, err
	}
	return hash, key, nil
}

// ResetPassword sets the login password directly in the database, creating the
// account if there isn't one yet. It is the way back in after a forgotten
// password: binbash has no usernames and no email, so there is nobody to send a
// reset link to, and the only authority left is whoever can reach the database
// file on the server.
//
// It rotates the session-signing key too, which signs out every device. That is
// deliberate: you are either locked out (nothing to lose) or resetting because
// something looked wrong (in which case revoking existing sessions is the whole
// point).
//
// This is a standalone function rather than a method because it runs from the
// command line, against a database no server is serving -- and it is why the
// caller must make sure of that. A running binbash caches the password hash and
// session key in memory, so it would carry on honouring the old password until
// it is restarted.
func ResetPassword(db *sql.DB, newPassword string) error {
	// Capitalised deliberately: unlike most Go errors this one is printed
	// straight to a person standing at a terminal, not wrapped into a sentence.
	if problem := ValidatePassword(newPassword); problem != "" {
		return errors.New(problem)
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("hash new password: %w", err)
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return fmt.Errorf("generate session key: %w", err)
	}

	// Upsert: this has to work whether the account exists (the forgotten-password
	// case) or not (a database whose auth row was cleared, or never created).
	_, err = db.Exec(`
		INSERT INTO auth_settings (id, password_hash, session_key) VALUES (1, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			password_hash = excluded.password_hash,
			session_key   = excluded.session_key`,
		string(hash), base64.StdEncoding.EncodeToString(key),
	)
	return err
}

// CheckPassword reports whether candidate matches the current password.
func (a *Auth) CheckPassword(candidate string) bool {
	a.mu.RLock()
	hash := a.passwordHash
	a.mu.RUnlock()
	// bcrypt comparison runs outside the lock since it's deliberately slow;
	// holding a read lock is fine concurrently with other readers, but there's
	// no reason to make a concurrent Rotate wait on it.
	return bcrypt.CompareHashAndPassword(hash, []byte(candidate)) == nil
}

// Rotate changes the password and, with it, the session-signing key: every
// session issued under the old key stops validating immediately. Callers must
// re-issue the current user's cookie with Login afterwards, or they will sign
// themselves out along with everyone else.
//
// Both values move in a single UPDATE, so a failure part-way cannot leave the
// stored hash and key disagreeing with the cached ones.
func (a *Auth) Rotate(newPassword string) error {
	hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("hash new password: %w", err)
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return fmt.Errorf("generate session key: %w", err)
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	if _, err := a.db.Exec(
		`UPDATE auth_settings SET password_hash = ?, session_key = ? WHERE id = 1`,
		string(hash), base64.StdEncoding.EncodeToString(key),
	); err != nil {
		return err
	}
	a.passwordHash = hash
	a.key = key
	return nil
}

// Login sets a signed session cookie on the response.
//
// The request is needed to decide the Secure flag: without it the browser will
// happily send the session cookie over any plain-HTTP request to the same
// host, which hands the session to anyone watching the network no matter how
// well the reverse proxy terminates TLS. It cannot simply always be set --
// browsers refuse to store a Secure cookie that arrives over HTTP, so doing
// that unconditionally would make login silently impossible on a plain-HTTP
// LAN install, which stays supported.
func (a *Auth) Login(w http.ResponseWriter, r *http.Request) {
	expiry := time.Now().Add(sessionTTL).Unix()
	payload := strconv.FormatInt(expiry, 10)
	sig := a.sign(payload)
	value := payload + "." + sig

	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    value,
		Path:     "/",
		HttpOnly: true,
		Secure:   a.proxies.IsHTTPS(r),
		SameSite: http.SameSiteLaxMode,
		Expires:  time.Now().Add(sessionTTL),
	})
}

// Logout clears the session cookie. It mirrors Login's attributes, Secure
// included, so that the clearing cookie isn't itself rejected as insecure.
func (a *Auth) Logout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   a.proxies.IsHTTPS(r),
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
}

// IsAuthenticated reports whether the request carries a valid, unexpired session cookie.
func (a *Auth) IsAuthenticated(r *http.Request) bool {
	c, err := r.Cookie(cookieName)
	if err != nil || c.Value == "" {
		return false
	}

	dot := -1
	for i := len(c.Value) - 1; i >= 0; i-- {
		if c.Value[i] == '.' {
			dot = i
			break
		}
	}
	if dot < 0 {
		return false
	}
	payload, sig := c.Value[:dot], c.Value[dot+1:]

	if subtle.ConstantTimeCompare([]byte(sig), []byte(a.sign(payload))) != 1 {
		return false
	}

	expiry, err := strconv.ParseInt(payload, 10, 64)
	if err != nil {
		return false
	}

	return time.Now().Unix() < expiry
}

func (a *Auth) sign(payload string) string {
	a.mu.RLock()
	key := a.key
	a.mu.RUnlock()

	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// ClientIP returns the address to attribute the request to -- the real client
// behind a trusted reverse proxy, otherwise the connecting peer. See
// TrustedProxies.
func (a *Auth) ClientIP(r *http.Request) string {
	return a.proxies.ClientIP(r)
}

// Throttled reports whether ip is currently locked out after too many failed
// login attempts, and if so, how much longer the lockout lasts.
func (a *Auth) Throttled(ip string) (wait time.Duration, blocked bool) {
	a.throttleMu.Lock()
	defer a.throttleMu.Unlock()

	rec, ok := a.attempts[ip]
	if !ok {
		return 0, false
	}
	if remaining := time.Until(rec.lockedUntil); remaining > 0 {
		return remaining, true
	}
	return 0, false
}

// RecordFailure counts a failed login attempt from ip, locking it out for
// lockoutDuration once MaxAttempts is reached within throttleWindow. Callers
// should only invoke this for attempts that weren't already blocked by
// Throttled, so a lockout in progress doesn't keep sliding its own window.
//
// It reports whether this attempt is the one that tripped the lockout, so the
// caller can log that transition exactly once.
func (a *Auth) RecordFailure(ip string) (lockedOut bool) {
	a.throttleMu.Lock()
	defer a.throttleMu.Unlock()
	a.sweepLocked()

	now := time.Now()
	rec, ok := a.attempts[ip]
	if !ok || now.Sub(rec.windowStart) > throttleWindow {
		rec = &attempt{windowStart: now}
		a.attempts[ip] = rec
	}
	rec.count++
	rec.lastSeen = now
	if rec.count >= MaxAttempts {
		rec.lockedUntil = now.Add(lockoutDuration)
		return rec.count == MaxAttempts
	}
	return false
}

// RecordSuccess clears any throttle state for ip after a successful login.
func (a *Auth) RecordSuccess(ip string) {
	a.throttleMu.Lock()
	defer a.throttleMu.Unlock()
	delete(a.attempts, ip)
}

// sweepLocked removes throttle records idle long enough to no longer be
// relevant, keeping the map bounded. Callers must hold throttleMu.
func (a *Auth) sweepLocked() {
	now := time.Now()
	for ip, rec := range a.attempts {
		if now.Sub(rec.lastSeen) > throttleIdleTTL {
			delete(a.attempts, ip)
		}
	}
}
