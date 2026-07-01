// Package auth implements binbash's single-password login and session cookie handling.
package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	cookieName = "binbash_session"
	sessionTTL = 30 * 24 * time.Hour
)

// Auth checks passwords and issues/validates signed session cookies. The
// signing key is derived from the configured password, so sessions remain
// valid across restarts without needing separate secret storage.
type Auth struct {
	password string
	key      []byte
}

func New(password string) *Auth {
	sum := sha256.Sum256([]byte("binbash-session-key:" + password))
	return &Auth{password: password, key: sum[:]}
}

// CheckPassword reports whether the supplied password matches the configured one.
func (a *Auth) CheckPassword(candidate string) bool {
	return subtle.ConstantTimeCompare([]byte(candidate), []byte(a.password)) == 1
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
	mac := hmac.New(sha256.New, a.key)
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
