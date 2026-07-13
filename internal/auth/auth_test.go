package auth

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/thinkscotty/binbash/internal/db"
)

// newTestAuth builds an Auth over a throwaway database, trusting the proxies
// given (or loopback, when none are).
func newTestAuth(t *testing.T, trusted ...string) *Auth {
	t.Helper()

	database, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	t.Cleanup(func() { database.Close() })

	var proxies *TrustedProxies
	if len(trusted) == 0 {
		proxies = DefaultTrustedProxies()
	} else {
		proxies, err = ParseTrustedProxies(trusted)
		if err != nil {
			t.Fatalf("parse trusted proxies %v: %v", trusted, err)
		}
	}

	a, err := New(database, "bootstrap-password", proxies)
	if err != nil {
		t.Fatalf("new auth: %v", err)
	}
	return a
}

func TestParseTrustedProxies(t *testing.T) {
	tests := []struct {
		name    string
		specs   []string
		wantErr bool
	}{
		{"bare IPv4", []string{"127.0.0.1"}, false},
		{"bare IPv6", []string{"::1"}, false},
		{"CIDR range", []string{"172.17.0.0/16"}, false},
		{"mixed, with blanks", []string{"127.0.0.1", "", " 10.0.0.0/8 "}, false},
		{"empty list trusts nothing", nil, false},
		{"not an address", []string{"proxy.example.com"}, true},
		{"nonsense", []string{"banana"}, true},
		{"malformed CIDR", []string{"10.0.0.0/64"}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseTrustedProxies(tt.specs)
			if gotErr := err != nil; gotErr != tt.wantErr {
				t.Fatalf("ParseTrustedProxies(%q) error = %v, wantErr %v", tt.specs, err, tt.wantErr)
			}
		})
	}
}

// TestClientIPIgnoresForgedHeaders is the important half of the throttle's
// correctness: an attacker who could pick their own client IP would get a
// fresh five-guess budget on every request, so X-Forwarded-For must be ignored
// unless the connection itself came from a proxy we trust.
func TestClientIPIgnoresForgedHeaders(t *testing.T) {
	a := newTestAuth(t) // loopback only

	r := httptest.NewRequest(http.MethodPost, "/login", nil)
	r.RemoteAddr = "203.0.113.9:54321" // straight off the internet, not a proxy
	r.Header.Set("X-Forwarded-For", "1.2.3.4")

	if got := a.ClientIP(r); got != "203.0.113.9" {
		t.Fatalf("ClientIP() = %q, want the real peer 203.0.113.9 -- a forged X-Forwarded-For was believed", got)
	}
}

func TestClientIP(t *testing.T) {
	tests := []struct {
		name       string
		trusted    []string
		remoteAddr string
		forwarded  []string
		want       string
	}{
		{
			name:       "no proxy, no headers: the peer",
			remoteAddr: "192.168.1.50:41234",
			want:       "192.168.1.50",
		},
		{
			name:       "trusted loopback proxy forwards the client",
			remoteAddr: "127.0.0.1:41234",
			forwarded:  []string{"198.51.100.7"},
			want:       "198.51.100.7",
		},
		{
			// The right-most entry is the one our trusted proxy appended, so
			// it is the only one it vouches for. Everything to its left was
			// supplied by the caller and could say anything.
			name:       "prepended chain: only the proxy's own entry counts",
			remoteAddr: "127.0.0.1:41234",
			forwarded:  []string{"1.2.3.4, 198.51.100.7"},
			want:       "198.51.100.7",
		},
		{
			// Two trusted hops (edge proxy -> local proxy): skip over both and
			// take the first address that isn't one of ours.
			name:       "chained trusted proxies are skipped",
			trusted:    []string{"127.0.0.1", "10.0.0.0/8"},
			remoteAddr: "127.0.0.1:41234",
			forwarded:  []string{"198.51.100.7, 10.0.0.5"},
			want:       "198.51.100.7",
		},
		{
			name:       "header split across lines",
			remoteAddr: "127.0.0.1:41234",
			forwarded:  []string{"1.2.3.4", "198.51.100.7"},
			want:       "198.51.100.7",
		},
		{
			// Garbage can only appear left of the proxy's own entry, so
			// stopping at it never discards a trustworthy address.
			name:       "malformed entry stops the walk",
			trusted:    []string{"127.0.0.1", "10.0.0.0/8"},
			remoteAddr: "127.0.0.1:41234",
			forwarded:  []string{"not-an-ip, 10.0.0.5"},
			want:       "127.0.0.1",
		},
		{
			name:       "trusted proxy sends no header",
			remoteAddr: "127.0.0.1:41234",
			want:       "127.0.0.1",
		},
		{
			name:       "IPv4-mapped IPv6 peer is recognised as loopback",
			remoteAddr: "[::ffff:127.0.0.1]:41234",
			forwarded:  []string{"198.51.100.7"},
			want:       "198.51.100.7",
		},
		{
			name:       "docker bridge proxy, once trusted",
			trusted:    []string{"172.17.0.0/16"},
			remoteAddr: "172.17.0.3:41234",
			forwarded:  []string{"198.51.100.7"},
			want:       "198.51.100.7",
		},
		{
			// Same request, but the operator never listed the bridge network:
			// we fall back to the peer rather than believing the header.
			name:       "untrusted docker bridge proxy is not believed",
			remoteAddr: "172.17.0.3:41234",
			forwarded:  []string{"198.51.100.7"},
			want:       "172.17.0.3",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := newTestAuth(t, tt.trusted...)

			r := httptest.NewRequest(http.MethodGet, "/", nil)
			r.RemoteAddr = tt.remoteAddr
			for _, line := range tt.forwarded {
				r.Header.Add("X-Forwarded-For", line)
			}

			if got := a.ClientIP(r); got != tt.want {
				t.Errorf("ClientIP() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestIsHTTPS(t *testing.T) {
	tests := []struct {
		name       string
		trusted    []string
		remoteAddr string
		proto      string
		tls        bool
		want       bool
	}{
		{"plain HTTP on the LAN", nil, "192.168.1.50:41234", "", false, false},
		{"direct TLS", nil, "192.168.1.50:41234", "", true, true},
		{"trusted proxy terminated TLS", nil, "127.0.0.1:41234", "https", false, true},
		{"trusted proxy served plain HTTP", nil, "127.0.0.1:41234", "http", false, false},
		{"proto header is case-insensitive", nil, "127.0.0.1:41234", "HTTPS", false, true},
		{"chained protos take the client's", nil, "127.0.0.1:41234", "https, http", false, true},
		// The whole point: a forged header from a non-proxy must not convince
		// us the connection was secure, or we would mark the session cookie
		// Secure on a plain-HTTP install and lock the user out.
		{"forged proto from an untrusted peer", nil, "203.0.113.9:41234", "https", false, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := newTestAuth(t, tt.trusted...)

			r := httptest.NewRequest(http.MethodGet, "/", nil)
			r.RemoteAddr = tt.remoteAddr
			if tt.proto != "" {
				r.Header.Set("X-Forwarded-Proto", tt.proto)
			}
			if tt.tls {
				r.TLS = &tls.ConnectionState{}
			}

			if got := a.proxies.IsHTTPS(r); got != tt.want {
				t.Errorf("IsHTTPS() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestLoginCookieSecureFlag pins both directions of the Secure decision. Set it
// when it shouldn't be and a plain-HTTP LAN user can never log in (the browser
// discards the cookie); leave it off when it should be on and the session
// cookie will ride along on any plain-HTTP request to the host.
func TestLoginCookieSecureFlag(t *testing.T) {
	tests := []struct {
		name       string
		remoteAddr string
		proto      string
		wantSecure bool
	}{
		{"plain HTTP LAN install", "192.168.1.50:41234", "", false},
		{"behind a TLS-terminating proxy", "127.0.0.1:41234", "https", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := newTestAuth(t)

			r := httptest.NewRequest(http.MethodPost, "/login", nil)
			r.RemoteAddr = tt.remoteAddr
			if tt.proto != "" {
				r.Header.Set("X-Forwarded-Proto", tt.proto)
			}
			w := httptest.NewRecorder()

			a.Login(w, r)

			cookie := sessionCookie(t, w.Result().Cookies())
			if cookie.Secure != tt.wantSecure {
				t.Errorf("cookie Secure = %v, want %v", cookie.Secure, tt.wantSecure)
			}
			if !cookie.HttpOnly {
				t.Error("cookie HttpOnly = false, want true")
			}
			if cookie.SameSite != http.SameSiteLaxMode {
				t.Errorf("cookie SameSite = %v, want Lax", cookie.SameSite)
			}
		})
	}
}

// TestRotateInvalidatesOtherSessions is the guarantee that makes "change your
// password" a real remedy for a leaked session cookie. Before this, the
// session-signing key never changed, so a stolen cookie stayed valid for its
// full 30-day life no matter how many times the password was changed.
func TestRotateInvalidatesOtherSessions(t *testing.T) {
	a := newTestAuth(t)

	// A session issued before the password change -- think: the attacker's.
	r := httptest.NewRequest(http.MethodPost, "/login", nil)
	w := httptest.NewRecorder()
	a.Login(w, r)
	old := sessionCookie(t, w.Result().Cookies())

	signedIn := httptest.NewRequest(http.MethodGet, "/", nil)
	signedIn.AddCookie(old)
	if !a.IsAuthenticated(signedIn) {
		t.Fatal("session should be valid before the password change")
	}

	if err := a.Rotate("a-brand-new-password"); err != nil {
		t.Fatalf("rotate: %v", err)
	}

	if a.IsAuthenticated(signedIn) {
		t.Error("session signed with the old key still authenticates after a password change")
	}

	// And the freshly issued cookie -- the one ChangePassword hands back to
	// whoever made the change -- still works, so they aren't logged out too.
	w = httptest.NewRecorder()
	a.Login(w, httptest.NewRequest(http.MethodPost, "/settings/password", nil))
	reissued := httptest.NewRequest(http.MethodGet, "/", nil)
	reissued.AddCookie(sessionCookie(t, w.Result().Cookies()))
	if !a.IsAuthenticated(reissued) {
		t.Error("cookie re-issued after rotation does not authenticate")
	}
}

func TestRotateChangesPassword(t *testing.T) {
	a := newTestAuth(t)

	if !a.CheckPassword("bootstrap-password") {
		t.Fatal("bootstrap password should be accepted before rotation")
	}
	if err := a.Rotate("a-brand-new-password"); err != nil {
		t.Fatalf("rotate: %v", err)
	}
	if a.CheckPassword("bootstrap-password") {
		t.Error("old password still accepted after rotation")
	}
	if !a.CheckPassword("a-brand-new-password") {
		t.Error("new password not accepted after rotation")
	}
}

func TestRecordFailureReportsLockout(t *testing.T) {
	a := newTestAuth(t)

	for i := 1; i < MaxAttempts; i++ {
		if locked := a.RecordFailure("198.51.100.7"); locked {
			t.Fatalf("locked out after %d attempts, want %d", i, MaxAttempts)
		}
		if _, blocked := a.Throttled("198.51.100.7"); blocked {
			t.Fatalf("throttled after %d attempts, want %d", i, MaxAttempts)
		}
	}

	if locked := a.RecordFailure("198.51.100.7"); !locked {
		t.Errorf("RecordFailure did not report the lockout on attempt %d", MaxAttempts)
	}
	if _, blocked := a.Throttled("198.51.100.7"); !blocked {
		t.Errorf("not throttled after %d attempts", MaxAttempts)
	}
	// Throttling is per-IP, so one attacker must not lock out everyone else.
	if _, blocked := a.Throttled("203.0.113.1"); blocked {
		t.Error("an unrelated IP was throttled")
	}
}

func TestSecurityHeaders(t *testing.T) {
	a := newTestAuth(t)
	handler := a.Protect(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))

	want := map[string]string{
		"X-Content-Type-Options": "nosniff",
		"X-Frame-Options":        "DENY",
		"Referrer-Policy":        "same-origin",
	}

	t.Run("plain HTTP", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodGet, "/login", nil)
		r.RemoteAddr = "192.168.1.50:41234"
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)

		for header, value := range want {
			if got := w.Header().Get(header); got != value {
				t.Errorf("%s = %q, want %q", header, got, value)
			}
		}
		if csp := w.Header().Get("Content-Security-Policy"); !strings.Contains(csp, "default-src 'self'") {
			t.Errorf("Content-Security-Policy = %q, want it to contain default-src 'self'", csp)
		}
		// HSTS over plain HTTP would be pointless at best; if the host ever
		// served TLS and then stopped, it would lock users out for a year.
		if hsts := w.Header().Get("Strict-Transport-Security"); hsts != "" {
			t.Errorf("Strict-Transport-Security = %q on a plain-HTTP request, want none", hsts)
		}
	})

	t.Run("behind a TLS proxy", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodGet, "/login", nil)
		r.RemoteAddr = "127.0.0.1:41234"
		r.Header.Set("X-Forwarded-Proto", "https")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)

		if hsts := w.Header().Get("Strict-Transport-Security"); hsts == "" {
			t.Error("Strict-Transport-Security missing on an HTTPS request")
		}
	})
}

// TestCSRFProtection covers the second lock on every destructive endpoint --
// import-with-replace wipes the whole inventory, and SameSite=Lax was until now
// the only thing standing between it and a malicious page.
func TestCSRFProtection(t *testing.T) {
	tests := []struct {
		name       string
		method     string
		origin     string
		secFetch   string
		wantStatus int
	}{
		{"same-origin POST", http.MethodPost, "http://binbash.example", "same-origin", http.StatusOK},
		{"cross-site POST is rejected", http.MethodPost, "http://evil.example", "cross-site", http.StatusForbidden},
		{"cross-site POST without Sec-Fetch-Site falls back to Origin", http.MethodPost, "http://evil.example", "", http.StatusForbidden},
		{"browser navigation POST", http.MethodPost, "", "none", http.StatusOK},
		// Safe methods pass untouched, which is only sound because binbash has
		// no state-changing GET routes -- every mutation is a POST.
		{"cross-site GET is allowed", http.MethodGet, "http://evil.example", "cross-site", http.StatusOK},
		// No browser headers at all: a script or curl, carrying no ambient
		// session from a victim's browser to abuse.
		{"non-browser POST", http.MethodPost, "", "", http.StatusOK},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := newTestAuth(t)
			handler := a.Protect(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			}))

			// /login is the one state-changing route outside the auth gate, so
			// it exercises CSRF without needing a session.
			r := httptest.NewRequest(tt.method, "/login", nil)
			r.Host = "binbash.example"
			if tt.origin != "" {
				r.Header.Set("Origin", tt.origin)
			}
			if tt.secFetch != "" {
				r.Header.Set("Sec-Fetch-Site", tt.secFetch)
			}
			w := httptest.NewRecorder()

			handler.ServeHTTP(w, r)

			if w.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", w.Code, tt.wantStatus)
			}
		})
	}
}

// TestSanitizeForLog pins the defence against log injection. binbash's log is
// a security control -- fail2ban bans IPs from it -- so an attacker who can
// smuggle text into a log line can forge the very marker the ban rule looks
// for and have an IP of their choosing banned at the victim's own firewall.
func TestSanitizeForLog(t *testing.T) {
	tests := []struct {
		name  string
		in    string
		want  string
		notIn string
	}{
		{
			name:  "a forged auth-failure marker cannot survive",
			in:    "binbash: auth failure from 8.8.8.8: hi",
			notIn: "auth failure from 8.8.8.8",
		},
		{
			name:  "newlines cannot start a fake line",
			in:    "http://evil.example\nbinbash: auth failure from 8.8.8.8:",
			notIn: "\n",
		},
		{"an honest origin is untouched", "https://binbash.example:8443", "https://binbash.example:8443", ""},
		{"empty stays empty", "", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sanitizeForLog(tt.in)
			if tt.want != "" && got != tt.want {
				t.Errorf("sanitizeForLog(%q) = %q, want %q", tt.in, got, tt.want)
			}
			if tt.notIn != "" && strings.Contains(got, tt.notIn) {
				t.Errorf("sanitizeForLog(%q) = %q, which still contains %q", tt.in, got, tt.notIn)
			}
		})
	}

	// Length is bounded too, so a huge header can't flood the log.
	if got := sanitizeForLog(strings.Repeat("a", 5000)); len([]rune(got)) > 101 {
		t.Errorf("sanitizeForLog did not bound a 5000-character value: got %d runes", len([]rune(got)))
	}
}

// TestProtectStillGatesOnAuth guards against the CSRF and header layers
// accidentally swallowing the redirect that keeps the app private.
func TestProtectStillGatesOnAuth(t *testing.T) {
	a := newTestAuth(t)
	handler := a.Protect(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler reached without a session")
	}))

	r := httptest.NewRequest(http.MethodGet, "/bins", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want %d (redirect to /login)", w.Code, http.StatusSeeOther)
	}
	if loc := w.Header().Get("Location"); loc != "/login" {
		t.Errorf("Location = %q, want /login", loc)
	}
}

func sessionCookie(t *testing.T, cookies []*http.Cookie) *http.Cookie {
	t.Helper()
	for _, c := range cookies {
		if c.Name == cookieName {
			return c
		}
	}
	t.Fatalf("no %s cookie was set", cookieName)
	return nil
}
