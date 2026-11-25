// Package auth implements the session model for a single administrator
// account (credentials from configuration). A request is either admin (a valid
// session) or anonymous (none); the API edge rejects anonymous requests on
// every data route, so "anonymous" is only ever the state of someone who has
// not logged in yet, not a role that can read anything.
//
// Sessions are stateless HMAC-SHA256 signed tokens in the JWT compact form,
// carried by an HttpOnly cookie or an Authorization: Bearer header. Stateless
// means a restart does not log anyone out, and rotating SPLASH_AUTH_SECRET
// logs everyone out — both properties a single-admin app wants.
//
// Everything here is standard library: crypto/hmac, crypto/sha256,
// crypto/subtle, encoding/base64, encoding/json.
package auth

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// CookieName carries the session token.
const CookieName = "splash_session"

// Role is the access level of a request.
type Role string

// The two roles. Anonymous is the zero value, so a request that never went
// through the middleware fails closed rather than open.
const (
	RoleAnonymous Role = "anonymous"
	RoleAdmin     Role = "admin"
)

// Claims is the token payload.
type Claims struct {
	Subject  string `json:"sub"`
	IssuedAt int64  `json:"iat"`
	Expires  int64  `json:"exp"`
}

// Authenticator checks credentials and issues/verifies session tokens.
type Authenticator struct {
	username string
	passHash [32]byte
	secret   []byte
	ttl      time.Duration
	secure   bool
	clock    func() time.Time
}

// Options configures New. Clock may be nil (time.Now).
type Options struct {
	Username string
	Password string
	Secret   string
	TTL      time.Duration
	Secure   bool
	Clock    func() time.Time
}

// New builds an Authenticator. The password is kept only as a SHA-256 hash so
// a memory dump does not hand over the credential directly; comparison is
// constant-time on the hashes.
func New(opts Options) *Authenticator {
	clock := opts.Clock
	if clock == nil {
		clock = time.Now
	}
	ttl := opts.TTL
	if ttl <= 0 {
		ttl = 7 * 24 * time.Hour
	}
	return &Authenticator{
		username: opts.Username,
		passHash: sha256.Sum256([]byte(opts.Password)),
		secret:   []byte(opts.Secret),
		ttl:      ttl,
		secure:   opts.Secure,
		clock:    clock,
	}
}

// Check reports whether the credentials match. Both comparisons are
// constant-time so a wrong username and a wrong password take the same time.
func (a *Authenticator) Check(username, password string) bool {
	hash := sha256.Sum256([]byte(password))
	userOK := subtle.ConstantTimeCompare([]byte(username), []byte(a.username)) == 1
	passOK := subtle.ConstantTimeCompare(hash[:], a.passHash[:]) == 1
	return userOK && passOK
}

// jwtHeader is the fixed HS256 header, pre-encoded.
var jwtHeader = base64URL([]byte(`{"alg":"HS256","typ":"JWT"}`))

// Issue mints a signed token for the admin account.
func (a *Authenticator) Issue() (string, error) {
	now := a.clock()
	claims := Claims{
		Subject:  a.username,
		IssuedAt: now.Unix(),
		Expires:  now.Add(a.ttl).Unix(),
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", fmt.Errorf("auth: encode claims: %w", err)
	}
	signingInput := jwtHeader + "." + base64URL(payload)
	return signingInput + "." + base64URL(a.mac(signingInput)), nil
}

// ErrInvalidToken reports a token that is malformed, tampered with, or expired.
var ErrInvalidToken = errors.New("auth: invalid token")

// Parse verifies a token's signature and expiry and returns its claims.
func (a *Authenticator) Parse(token string) (Claims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return Claims{}, ErrInvalidToken
	}
	signingInput := parts[0] + "." + parts[1]
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return Claims{}, ErrInvalidToken
	}
	// Compare raw MAC bytes to raw MAC bytes — comparing decoded bytes against
	// a base64 string would never match.
	if !hmac.Equal(sig, a.mac(signingInput)) {
		return Claims{}, ErrInvalidToken
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return Claims{}, ErrInvalidToken
	}
	var claims Claims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return Claims{}, ErrInvalidToken
	}
	if claims.Subject != a.username {
		return Claims{}, ErrInvalidToken
	}
	if a.clock().Unix() >= claims.Expires {
		return Claims{}, ErrInvalidToken
	}
	return claims, nil
}

func (a *Authenticator) mac(signingInput string) []byte {
	mac := hmac.New(sha256.New, a.secret)
	mac.Write([]byte(signingInput))
	return mac.Sum(nil)
}

func base64URL(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

// SetCookie writes the session cookie. It is a BROWSER-SESSION cookie (no
// MaxAge): closing the browser ends the session and forces a fresh login,
// which is the product requirement. The token's own expiry is the idle
// timeout, slid forward by activity (see Middleware), so an open editor never
// times out mid-edit.
//
// SameSite=Lax plus the JSON-only API is the CSRF posture: a cross-site form
// POST cannot carry this cookie, and a cross-site fetch cannot read the
// response without CORS.
func (a *Authenticator) SetCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   a.secure,
		SameSite: http.SameSiteLaxMode,
	})
}

// ClearCookie removes the session cookie.
func (a *Authenticator) ClearCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   a.secure,
		SameSite: http.SameSiteLaxMode,
	})
}

type contextKey int

const (
	roleKey contextKey = iota
	usernameKey
)

// Middleware reads the session (cookie first, then Bearer header) and puts the
// role on the request context. An absent or invalid token is not an error here:
// the request proceeds as anonymous, and the API's guard rejects it on every
// data route. Only the auth door and the probes run without a session.
func (a *Authenticator) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		role, username := RoleAnonymous, ""
		token := ""
		if cookie, err := r.Cookie(CookieName); err == nil {
			token = cookie.Value
		}
		if token == "" {
			if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
				token = strings.TrimPrefix(h, "Bearer ")
			}
		}
		if token != "" {
			if claims, err := a.Parse(token); err == nil {
				role, username = RoleAdmin, claims.Subject
				// Sliding expiration: once less than half the idle window
				// remains, any authenticated request reissues the token. An
				// active user (or an open editor's heartbeat) never expires;
				// an idle one does, after SPLASH_SESSION_TTL.
				if claims.Expires-a.clock().Unix() < int64(a.ttl.Seconds())/2 {
					if fresh, err := a.Issue(); err == nil {
						a.SetCookie(w, fresh)
					}
				}
			}
		}
		ctx := context.WithValue(r.Context(), roleKey, role)
		ctx = context.WithValue(ctx, usernameKey, username)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// RoleFrom returns the request's role, or RoleAnonymous when the middleware is
// not installed — failing closed.
func RoleFrom(ctx context.Context) Role {
	role, _ := ctx.Value(roleKey).(Role)
	if role == "" {
		return RoleAnonymous
	}
	return role
}

// UsernameFrom returns the authenticated username, or "".
func UsernameFrom(ctx context.Context) string {
	name, _ := ctx.Value(usernameKey).(string)
	return name
}

// IsAdmin reports whether ctx carries the admin role.
func IsAdmin(ctx context.Context) bool { return RoleFrom(ctx) == RoleAdmin }
