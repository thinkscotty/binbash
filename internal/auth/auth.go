// Package auth implements binbash's single-password login, session cookie
// handling, in-app password rotation, and brute-force login throttling.
package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
)

const (
	cookieName = "binbash_session"
	sessionTTL = 30 * 24 * time.Hour

	maxAttempts     = 5
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
// The session-signing key is intentionally independent of the password: it
// is generated once at bootstrap and never changes, so rotating the password
// does not invalidate existing sessions, including the session of whoever
// just changed it.
type Auth struct {
	db *sql.DB

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
func New(db *sql.DB, bootstrapPassword string) (*Auth, error) {
	hash, key, err := loadSettings(db)
	if err != nil {
		return nil, fmt.Errorf("load auth settings: %w", err)
	}
	if hash == nil {
		hash, key, err = bootstrap(db, bootstrapPassword)
		if err != nil {
			return nil, fmt.Errorf("bootstrap auth settings: %w", err)
		}
	}

	return &Auth{
		db:           db,
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

// Rotate changes the password. The session-signing key is left untouched, so
// existing sessions -- including the caller's own -- remain valid.
func (a *Auth) Rotate(newPassword string) error {
	hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("hash new password: %w", err)
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	if _, err := a.db.Exec(`UPDATE auth_settings SET password_hash = ? WHERE id = 1`, string(hash)); err != nil {
		return err
	}
	a.passwordHash = hash
	return nil
}

// Login sets a signed session cookie on the response.
func (a *Auth) Login(w http.ResponseWriter) {
	expiry := time.Now().Add(sessionTTL).Unix()
	payload := strconv.FormatInt(expiry, 10)
	sig := a.sign(payload)
	value := payload + "." + sig

	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    value,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Expires:  time.Now().Add(sessionTTL),
	})
}

// Logout clears the session cookie.
func (a *Auth) Logout(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
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

// Middleware redirects unauthenticated requests to /login, except for the
// login route and static assets.
func (a *Auth) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/login" || strings.HasPrefix(r.URL.Path, "/static/") {
			next.ServeHTTP(w, r)
			return
		}

		if !a.IsAuthenticated(r) {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// ClientIP extracts the request's client address for throttle keying,
// stripping the port. Behind a reverse proxy that doesn't forward the
// original client address, every request will share the proxy's IP -- this
// degrades throttling to a single shared bucket rather than defeating it.
func (a *Auth) ClientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
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
// lockoutDuration once maxAttempts is reached within throttleWindow. Callers
// should only invoke this for attempts that weren't already blocked by
// Throttled, so a lockout in progress doesn't keep sliding its own window.
func (a *Auth) RecordFailure(ip string) {
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
	if rec.count >= maxAttempts {
		rec.lockedUntil = now.Add(lockoutDuration)
	}
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
