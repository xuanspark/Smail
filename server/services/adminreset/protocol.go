package adminreset

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"
)

const (
	ProtocolVersion       = 1
	maxControlMessageSize = 16 << 10
	controlRequestTimeout = 15 * time.Second
)

var (
	ErrUnauthorizedPeer = errors.New("unauthorized local peer")
	ErrInvalidRequest   = errors.New("invalid reset request")
	ErrReplay           = errors.New("reset request replayed")
	ErrExpiredSession   = errors.New("reset session expired")
	ErrUnsupported      = errors.New("local reset channel is unsupported")
)

type Request struct {
	Version     int    `json:"version"`
	Nonce       string `json:"nonce"`
	NewPassword string `json:"new_password"`
}

type Response struct {
	OK      bool   `json:"ok"`
	Code    string `json:"code,omitempty"`
	Message string `json:"message,omitempty"`
	Nonce   string `json:"nonce,omitempty"`
}

type ResetHandler func(newPassword string) error

type SessionGuard struct {
	mu      sync.Mutex
	pending map[string]time.Time
	ttl     time.Duration
	now     func() time.Time
}

func NewSessionGuard(ttl time.Duration) *SessionGuard {
	if ttl <= 0 {
		ttl = 2 * time.Minute
	}
	return &SessionGuard{pending: make(map[string]time.Time), ttl: ttl, now: time.Now}
}

func (g *SessionGuard) Issue() (string, error) {
	if g == nil {
		return "", ErrInvalidRequest
	}
	now := g.now()
	g.mu.Lock()
	defer g.mu.Unlock()
	g.pruneLocked(now)
	for attempt := 0; attempt < 3; attempt++ {
		nonce, err := NewNonce()
		if err != nil {
			return "", err
		}
		if _, exists := g.pending[nonce]; exists {
			continue
		}
		g.pending[nonce] = now.Add(g.ttl)
		return nonce, nil
	}
	return "", errors.New("reset nonce collision")
}

func (g *SessionGuard) Consume(nonce string) error {
	if g == nil || strings.TrimSpace(nonce) == "" {
		return ErrInvalidRequest
	}
	now := g.now()
	g.mu.Lock()
	defer g.mu.Unlock()
	expiry, exists := g.pending[nonce]
	if !exists {
		g.pruneLocked(now)
		return ErrReplay
	}
	delete(g.pending, nonce)
	if !now.Before(expiry) {
		return ErrExpiredSession
	}
	g.pruneLocked(now)
	return nil
}

func (g *SessionGuard) Discard(nonce string) {
	if g == nil || nonce == "" {
		return
	}
	g.mu.Lock()
	delete(g.pending, nonce)
	g.mu.Unlock()
}

func (g *SessionGuard) pruneLocked(now time.Time) {
	for nonce, expiry := range g.pending {
		if !now.Before(expiry) {
			delete(g.pending, nonce)
		}
	}
}

func NewNonce() (string, error) {
	var raw [24]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("generate reset nonce: %w", err)
	}
	return hex.EncodeToString(raw[:]), nil
}

func DecodeRequest(r io.Reader) (Request, error) {
	data, err := readControlFrame(r)
	if err != nil {
		return Request{}, ErrInvalidRequest
	}
	var request Request
	if err := decodeControlJSON(data, &request); err != nil {
		return Request{}, ErrInvalidRequest
	}
	if request.Version != ProtocolVersion || strings.TrimSpace(request.Nonce) == "" || len([]byte(request.NewPassword)) < 8 || len([]byte(request.NewPassword)) > 256 {
		return Request{}, ErrInvalidRequest
	}
	return request, nil
}

func DecodeResponse(r io.Reader) (Response, error) {
	data, err := readControlFrame(r)
	if err != nil {
		return Response{}, ErrInvalidRequest
	}
	var response Response
	if err := decodeControlJSON(data, &response); err != nil {
		return Response{}, ErrInvalidRequest
	}
	return response, nil
}

func readControlFrame(r io.Reader) ([]byte, error) {
	if r == nil {
		return nil, ErrInvalidRequest
	}
	reader := bufio.NewReaderSize(r, maxControlMessageSize+1)
	frame, err := reader.ReadSlice('\n')
	if errors.Is(err, bufio.ErrBufferFull) || len(frame) > maxControlMessageSize {
		return nil, ErrInvalidRequest
	}
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, ErrInvalidRequest
	}
	if len(bytes.TrimSpace(frame)) == 0 || reader.Buffered() != 0 {
		return nil, ErrInvalidRequest
	}
	return frame, nil
}

func decodeControlJSON(data []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return ErrInvalidRequest
	}
	return nil
}

func EncodeResponse(w io.Writer, response Response) error {
	return json.NewEncoder(w).Encode(response)
}

func handleResetConnection(conn io.ReadWriter, setDeadline func(time.Time) error, handler ResetHandler, guard *SessionGuard) {
	if conn == nil || handler == nil || guard == nil {
		return
	}
	if setDeadline != nil {
		_ = setDeadline(time.Now().Add(controlRequestTimeout))
		defer setDeadline(time.Time{})
	}
	nonce, err := guard.Issue()
	if err != nil {
		_ = EncodeResponse(conn, Response{Code: "CHANNEL_UNAVAILABLE", Message: "\u91cd\u7f6e\u901a\u9053\u4e0d\u53ef\u7528"})
		return
	}
	defer guard.Discard(nonce)
	if err := EncodeResponse(conn, Response{OK: true, Code: "CHALLENGE", Nonce: nonce}); err != nil {
		return
	}
	request, err := DecodeRequest(conn)
	if err != nil || request.Nonce != nonce {
		_ = EncodeResponse(conn, Response{Code: "INVALID_REQUEST", Message: "\u91cd\u7f6e\u8bf7\u6c42\u65e0\u6548"})
		return
	}
	if err := guard.Consume(request.Nonce); err != nil {
		_ = EncodeResponse(conn, Response{Code: "REPLAY_OR_EXPIRED", Message: "\u91cd\u7f6e\u4f1a\u8bdd\u5df2\u5931\u6548\u6216\u5df2\u4f7f\u7528"})
		return
	}
	password := request.NewPassword
	defer clearPassword(&password)
	if err := handler(password); err != nil {
		_ = EncodeResponse(conn, Response{Code: "RESET_FAILED", Message: "\u8d85\u7ea7\u7ba1\u7406\u5458\u5bc6\u7801\u91cd\u7f6e\u5931\u8d25"})
		return
	}
	_ = EncodeResponse(conn, Response{OK: true, Message: "\u8d85\u7ea7\u7ba1\u7406\u5458\u5bc6\u7801\u5df2\u91cd\u7f6e"})
}

func clearPassword(password *string) {
	if password == nil {
		return
	}
	buffer := []byte(*password)
	for i := range buffer {
		buffer[i] = 0
	}
	*password = ""
}
