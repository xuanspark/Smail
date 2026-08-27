package csrf

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"net/http"

	"git.pcux.cn/xuans/Smail/session"
)

const (
	CookieName = "smail_csrf"
	HeaderName = "X-CSRF-Token"
	SessionKey = "csrf_token"
)

func Issue(w http.ResponseWriter, r *http.Request, secure bool) (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	session.Instance.Put(r.Context(), SessionKey, token)
	http.SetCookie(w, &http.Cookie{
		Name: CookieName, Value: token, Path: "/", MaxAge: 86400,
		Secure: secure, HttpOnly: false, SameSite: http.SameSiteStrictMode,
	})
	return token, nil
}

func Clear(w http.ResponseWriter, r *http.Request, secure bool) {
	if session.Instance != nil {
		session.Instance.Remove(r.Context(), SessionKey)
	}
	http.SetCookie(w, &http.Cookie{
		Name: CookieName, Value: "", Path: "/", MaxAge: -1,
		Secure: secure, HttpOnly: false, SameSite: http.SameSiteStrictMode,
	})
}

func Valid(r *http.Request) bool {
	if session.Instance == nil {
		return false
	}
	header := r.Header.Get(HeaderName)
	cookie, err := r.Cookie(CookieName)
	if err != nil || header == "" || cookie.Value == "" {
		return false
	}
	stored := session.Instance.GetString(r.Context(), SessionKey)
	if stored == "" || len(stored) != len(header) || len(cookie.Value) != len(header) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(stored), []byte(header)) == 1 &&
		subtle.ConstantTimeCompare([]byte(cookie.Value), []byte(header)) == 1
}
