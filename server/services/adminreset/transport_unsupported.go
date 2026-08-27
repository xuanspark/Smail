//go:build !linux && !windows

package adminreset

import (
	"context"
	"net"
)

const DefaultControlPath = ""

func DialControl(context.Context, string) (net.Conn, error) { return nil, ErrUnsupported }
