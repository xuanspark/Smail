package adminreset

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"
)

var (
	ErrVerificationUnavailable = errors.New("no verification method available")
	ErrVerificationFailed      = errors.New("verification failed")
)

func responseError(code string) error {
	switch code {
	case "VERIFICATION_UNAVAILABLE":
		return ErrVerificationUnavailable
	case "VERIFICATION_FAILED":
		return ErrVerificationFailed
	case "REPLAY_OR_EXPIRED":
		return ErrExpiredSession
	default:
		return ErrInvalidRequest
	}
}

func SendVerifiedReset(ctx context.Context, conn net.Conn, input func(string) (string, error), readPassword func() (string, error)) error {
	if conn == nil || input == nil || readPassword == nil {
		return ErrInvalidRequest
	}
	deadline := time.Now().Add(controlRequestTimeout)
	if contextDeadline, ok := ctx.Deadline(); ok && contextDeadline.Before(deadline) {
		deadline = contextDeadline
	}
	if err := conn.SetDeadline(deadline); err != nil {
		return fmt.Errorf("set reset deadline: %w", err)
	}
	defer conn.SetDeadline(time.Time{})
	challenge, err := DecodeResponse(conn)
	if err != nil {
		return err
	}
	if !challenge.OK {
		return responseError(challenge.Code)
	}
	if challenge.Code != "CHALLENGE" || challenge.Nonce == "" {
		return ErrInvalidRequest
	}
	var prompt strings.Builder
	prompt.WriteString("请选择安全验证方式\n\n")
	for i, method := range challenge.Methods {
		fmt.Fprintf(&prompt, "%d. %s\n", i+1, method.Label)
	}
	prompt.WriteString("\n选项：")
	choice, err := input(prompt.String())
	if err != nil {
		return err
	}
	index, err := strconv.Atoi(strings.TrimSpace(choice))
	if err != nil || index < 1 || index > len(challenge.Methods) {
		return ErrInvalidRequest
	}
	exchange := func(request Request) error {
		request.Version, request.Nonce = ProtocolVersion, challenge.Nonce
		if err := json.NewEncoder(conn).Encode(request); err != nil {
			return err
		}
		response, err := DecodeResponse(conn)
		if err != nil {
			return err
		}
		if !response.OK {
			return responseError(response.Code)
		}
		return nil
	}
	if err := exchange(Request{Action: "select", Method: challenge.Methods[index-1].ID}); err != nil {
		return err
	}
	code, err := input("请输入所选方式的验证码或安全码")
	if err != nil {
		return err
	}
	err = exchange(Request{Action: "verify", Code: code})
	clearPassword(&code)
	if err != nil {
		return err
	}
	value, err := readPassword()
	if err != nil {
		return err
	}
	defer clearPassword(&value)
	if err := json.NewEncoder(conn).Encode(Request{Version: ProtocolVersion, Nonce: challenge.Nonce, Action: "reset", NewPassword: value}); err != nil {
		return fmt.Errorf("send reset request: %w", err)
	}
	response, err := DecodeResponse(conn)
	if err != nil {
		return fmt.Errorf("read reset response: %w", err)
	}
	if !response.OK {
		return responseError(response.Code)
	}
	return nil
}
