package csrf

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
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

// Ensure 为已认证会话恢复双提交 CSRF Cookie。
// Cookie 缺失或与服务端会话不一致时轮换令牌，避免继续信任过期的浏览器状态。
func Ensure(w http.ResponseWriter, r *http.Request, secure bool) (string, error) {
	if session.Instance == nil {
		return "", errors.New("session manager is unavailable")
	}
	stored := session.Instance.GetString(r.Context(), SessionKey)
	cookie, err := r.Cookie(CookieName)
	if err == nil && stored != "" && constantTimeEqual(stored, cookie.Value) {
		return stored, nil
	}
	return Issue(w, r, secure)
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
	if stored == "" {
		return false
	}
	return constantTimeEqual(stored, header) && constantTimeEqual(cookie.Value, header)
}

func constantTimeEqual(left, right string) bool {
	return len(left) == len(right) && subtle.ConstantTimeCompare([]byte(left), []byte(right)) == 1
}
