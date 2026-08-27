package adminreset

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"time"
)

func SendReset(ctx context.Context, conn net.Conn, password string) error {
	if conn == nil || password == "" {
		return ErrInvalidRequest
	}
	defer clearPassword(&password)
	deadline := time.Now().Add(10 * time.Second)
	if contextDeadline, ok := ctx.Deadline(); ok && contextDeadline.Before(deadline) {
		deadline = contextDeadline
	}
	if err := conn.SetDeadline(deadline); err != nil {
		return fmt.Errorf("set reset deadline: %w", err)
	}
	defer conn.SetDeadline(time.Time{})
	challenge, err := DecodeResponse(conn)
	if err != nil || !challenge.OK || challenge.Code != "CHALLENGE" || challenge.Nonce == "" {
		return ErrInvalidRequest
	}
	if err := json.NewEncoder(conn).Encode(Request{Version: ProtocolVersion, Nonce: challenge.Nonce, NewPassword: password}); err != nil {
		return fmt.Errorf("send reset request: %w", err)
	}
	response, err := DecodeResponse(conn)
	if err != nil {
		return fmt.Errorf("read reset response: %w", err)
	}
	if !response.OK {
		return fmt.Errorf("reset rejected: %s", response.Code)
	}
	return nil
}
