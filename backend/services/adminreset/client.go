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
	ErrEmailDeliveryFailed     = errors.New("verification email delivery failed")
)

func responseError(code string) error {
	switch code {
	case "VERIFICATION_UNAVAILABLE":
		return ErrVerificationUnavailable
	case "VERIFICATION_FAILED":
		return ErrVerificationFailed
	case "EMAIL_DELIVERY_FAILED":
		return ErrEmailDeliveryFailed
	case "REPLAY_OR_EXPIRED":
		return ErrExpiredSession
	default:
		return ErrInvalidRequest
	}
}

func SendVerifiedReset(ctx context.Context, conn net.Conn, input func(string) (string, error), readPassword func() (string, error)) error {
	if input == nil {
		return ErrInvalidRequest
	}
	return SendVerifiedResetWithInput(ctx, conn, func(_ context.Context, prompt string) (string, error) {
		return input(prompt)
	}, readPassword)
}

// SendVerifiedResetWithInput 允许邮件发送与验证码输入并行，发送失败会取消输入。
func SendVerifiedResetWithInput(ctx context.Context, conn net.Conn, input func(context.Context, string) (string, error), readPassword func() (string, error)) error {
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
	choice, err := input(ctx, prompt.String())
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
	method := challenge.Methods[index-1].ID
	request := Request{Version: ProtocolVersion, Nonce: challenge.Nonce, Action: "select", Method: method}
	var code string
	if strings.HasPrefix(method, "email:") {
		code, err = selectEmailAndReadCode(ctx, conn, request, input)
	} else {
		if err = exchange(request); err != nil {
			return err
		}
		code, err = input(ctx, "请输入所选方式的验证码或安全码")
	}
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

func selectEmailAndReadCode(ctx context.Context, conn net.Conn, request Request, input func(context.Context, string) (string, error)) (string, error) {
	if err := json.NewEncoder(conn).Encode(request); err != nil {
		return "", err
	}
	inputContext, cancel := context.WithCancel(ctx)
	defer cancel()
	result := make(chan error, 1)
	go func() {
		response, err := DecodeResponse(conn)
		if err == nil && !response.OK {
			err = responseError(response.Code)
		}
		if err != nil {
			cancel()
		}
		result <- err
	}()
	code, inputErr := input(inputContext, "正在发送邮件验证码，收到后输入\n请输入邮件验证码")
	if inputErr != nil {
		// 取消输入时关闭本次连接，同时解除等待投递结果的读取。
		_ = conn.Close()
	}
	deliveryErr := <-result
	if deliveryErr != nil || inputErr != nil {
		clearPassword(&code)
		if deliveryErr != nil {
			return "", deliveryErr
		}
		return "", inputErr
	}
	return code, nil
}
