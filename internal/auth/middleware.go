package auth

import (
	"log"
	"net/http"
	"strings"
)

// contentSecurityPolicy is deliberately strict, which binbash can afford
// because it serves nothing but its own assets: one stylesheet, one script
// (htmx), two self-hosted fonts, and zero inline scripts or styles. Each
// directive is here to close a specific door:
//
//	default-src 'self'    nothing loads from anywhere but binbash itself, so
//	                      an injected tag can't pull in or beacon out to a
//	                      third-party host
//	frame-ancestors 'none' the page cannot be framed -- the modern half of the
//	                      clickjacking defence that stops Delete and Update Now
//	                      being clicked through an invisible overlay
//	form-action 'self'    an injected form cannot post the CSRF-able actions
//	                      (or the password field) off to another origin
//	base-uri 'none'       an injected <base> cannot silently repoint every
//	                      relative URL on the page
//	object-src 'none'     no plugin content, ever
//
// htmx would otherwise inject a <style> element for its loading indicators at
// startup, which style-src 'self' blocks; layout.html turns that off via the
// htmx-config meta tag. binbash uses no indicators, so nothing is lost.
const contentSecurityPolicy = "default-src 'self'; " +
	"frame-ancestors 'none'; " +
	"form-action 'self'; " +
	"base-uri 'none'; " +
	"object-src 'none'"

// hstsMaxAge is one year, the value HSTS preload lists expect. It is sent only
// on requests that actually arrived over TLS: sending it on a plain-HTTP LAN
// install would be ignored by browsers anyway, but sending it on a host that
// later loses its certificate would lock users out of their own inventory for
// a year, so the condition matters. includeSubDomains is deliberately omitted
// -- binbash has no idea what else lives on the operator's domain, and forcing
// HTTPS on all of it is not its call to make.
const hstsMaxAge = "max-age=31536000"

// Protect wraps the application's routes in the full request-level defence
// chain, outermost first:
//
//  1. security headers -- set on every response, including the error responses
//     produced by the two layers below;
//  2. CSRF -- rejects cross-origin state-changing requests before they can
//     reach a handler;
//  3. authentication -- redirects anyone without a valid session to /login.
//
// The order is the point, and is why this is a single function rather than
// three the caller composes by hand: headers must be outside the layers that
// can short-circuit, and CSRF must be outside authentication so that POST
// /login -- which is exempt from the auth gate -- is still covered.
func (a *Auth) Protect(next http.Handler) http.Handler {
	return a.securityHeaders(a.csrfProtect(a.authenticate(next)))
}

// securityHeaders sets the response headers that constrain what a browser will
// do with a binbash page. They cost nothing and close off whole categories of
// attack (clickjacking, MIME-sniffing, referrer leakage, injected content) that
// are otherwise entirely undefended.
func (a *Auth) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", contentSecurityPolicy)
		// The legacy twin of frame-ancestors, for browsers that predate CSP
		// level 2. Harmless where frame-ancestors is understood -- it wins.
		h.Set("X-Frame-Options", "DENY")
		// Stop the browser second-guessing our Content-Type: without this, a
		// CSV export or an uploaded file served back could be sniffed into
		// something executable.
		h.Set("X-Content-Type-Options", "nosniff")
		// Bin and item names live in URLs. Same-origin keeps them out of the
		// Referer header on any request leaving binbash.
		h.Set("Referrer-Policy", "same-origin")

		if a.proxies.IsHTTPS(r) {
			h.Set("Strict-Transport-Security", hstsMaxAge)
		}

		next.ServeHTTP(w, r)
	})
}

// csrfProtect rejects state-changing requests that a browser tells us came
// from another site.
//
// SameSite=Lax on the session cookie already blocks cross-site POSTs in current
// browsers, but resting the whole defence on one browser-dependent mechanism is
// thin when the endpoints behind it will wipe an inventory (import-with-replace),
// delete a bin, change the password, or trigger a self-update. This is the
// second, independent lock.
//
// The stdlib's CrossOriginProtection does the checking: it trusts Sec-Fetch-Site
// where the browser sends it (every browser since 2023) and falls back to
// comparing Origin against Host. Both signals are set by the browser and cannot
// be forged by the attacking page. Safe methods pass untouched, which is only
// sound because binbash has no state-changing GET routes -- every mutation is a
// POST. Requests with neither header (curl, scripts) are allowed through, as
// they carry no ambient session from a victim's browser to abuse.
func (a *Auth) csrfProtect(next http.Handler) http.Handler {
	a.csrf.SetDenyHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Worth a log line: in normal use this fires zero times, so anything
		// here is either an attack or a proxy mangling Origin/Host.
		log.Printf("binbash: rejected cross-origin %s %s from origin %q", r.Method, r.URL.Path, sanitizeForLog(r.Header.Get("Origin")))
		http.Error(w, "This request looked like it came from another site, so binbash refused it.", http.StatusForbidden)
	}))
	return a.csrf.Handler(next)
}

// authenticate redirects unauthenticated requests to /login, except for the
// login route and static assets.
func (a *Auth) authenticate(next http.Handler) http.Handler {
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

// sanitizeForLog makes an attacker-supplied header value safe to write to the
// log, by keeping only printable non-space characters and bounding the length.
//
// This is not cosmetic. The log is a security control -- fail2ban bans IPs
// based on what it finds there -- and an attacker who can smuggle text into a
// log line can forge whatever that watcher is looking for. An Origin header of
// "binbash: auth failure from 8.8.8.8:" would otherwise appear verbatim in the
// line below and, matched loosely, get an IP of the attacker's choosing banned
// at the victim's firewall: their own, if they liked.
//
// Dropping whitespace is what defuses it, since every marker the shipped
// fail2ban filter looks for contains spaces and a real Origin
// (scheme://host:port) never does. The filter's regex is anchored to the start
// of the line as well, so either defence alone is sufficient.
func sanitizeForLog(s string) string {
	const max = 100

	var b strings.Builder
	for _, c := range []byte(s) {
		if b.Len() >= max {
			b.WriteString("…")
			break
		}
		if c > ' ' && c < 0x7f {
			b.WriteByte(c)
		}
	}
	return b.String()
}
