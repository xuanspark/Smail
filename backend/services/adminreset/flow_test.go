package adminreset

import (
	"context"
	"net"
)

// 协议测试使用固定凭据夹具；生产验证在独立数据库集成测试中覆盖。
type testResetFlow struct {
	reset              func(string) error
	selected, verified bool
}

func (f *testResetFlow) Methods() []Method { return []Method{{ID: "fixture", Label: "测试验证"}} }
func (f *testResetFlow) Start(method string) error {
	if method != "fixture" {
		return ErrInvalidRequest
	}
	f.selected = true
	return nil
}
func (f *testResetFlow) Verify(code string) error {
	if !f.selected || code != "fixture-code" {
		return ErrInvalidRequest
	}
	f.verified = true
	return nil
}
func (f *testResetFlow) Reset(value string) error {
	if !f.verified {
		return ErrInvalidRequest
	}
	f.verified = false
	return f.reset(value)
}
func (f *testResetFlow) Close() {}
func testResetHandler(reset func(string) error) ResetHandler {
	return func() (ResetSession, error) { return &testResetFlow{reset: reset}, nil }
}
func SendReset(ctx context.Context, conn net.Conn, value string) error {
	step := 0
	return SendVerifiedReset(ctx, conn, func(string) (string, error) {
		step++
		if step == 1 {
			return "1", nil
		}
		return "fixture-code", nil
	}, func() (string, error) { return value, nil })
}
