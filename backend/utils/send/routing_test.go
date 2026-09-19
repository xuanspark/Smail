package send

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"net/textproto"
	"reflect"
	"testing"

	"git.pcux.cn/xuans/Smail/utils/smtp"
)

func TestResolveMX(t *testing.T) {
	for _, tt := range []struct {
		name    string
		records []*net.MX
		err     error
		want    []string
		wantErr bool
	}{
		{name: "priority and duplicate", records: []*net.MX{{Host: "backup.example.", Pref: 20}, {Host: "primary.example.", Pref: 10}, {Host: "PRIMARY.example.", Pref: 30}}, want: []string{"primary.example", "backup.example"}},
		{name: "implicit MX", want: []string{"example.com"}},
		{name: "no MX record", err: &net.DNSError{IsNotFound: true}, want: []string{"example.com"}},
		{name: "temporary DNS", err: &net.DNSError{IsTemporary: true}, wantErr: true},
		{name: "timeout DNS", err: &net.DNSError{IsTimeout: true}, wantErr: true},
		{name: "null MX", records: []*net.MX{{Host: "."}}, wantErr: true},
		{name: "invalid MX", records: []*net.MX{nil}, wantErr: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			hosts, err := resolveMX("example.com", func(ctx context.Context, domain string) ([]*net.MX, error) {
				if _, ok := ctx.Deadline(); !ok || domain != "example.com" {
					t.Fatal("unbounded or wrong lookup")
				}
				return tt.records, tt.err
			})
			if (err != nil) != tt.wantErr || !reflect.DeepEqual(hosts, tt.want) {
				t.Fatalf("hosts=%v error=%v", hosts, err)
			}
		})
	}
}

func TestDeliverMXFallbackAndStop(t *testing.T) {
	for _, tt := range []struct {
		name  string
		err   error
		calls int
	}{
		{"network fallback", &net.OpError{Op: "dial", Err: io.EOF}, 4},
		{"temporary refusal fallback", &textproto.Error{Code: 451, Msg: "later"}, 4},
		{"permanent refusal", &textproto.Error{Code: 550, Msg: "rejected"}, 1},
		{"certificate failure", &tls.CertificateVerificationError{Err: io.EOF}, 1},
		{"uncertain DATA", &smtp.DeliveryUncertainError{Cause: io.EOF}, 1},
		{"private MX", smtp.ErrUnsafeRemoteAddress, 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var calls []string
			err := deliverMX([]string{"primary.example", "backup.example"}, func(host, port string) error {
				calls = append(calls, host+":"+port)
				if host == "backup.example" {
					return nil
				}
				return tt.err
			})
			if len(calls) != tt.calls {
				t.Fatalf("calls=%v", calls)
			}
			if tt.calls == 4 && err != nil {
				t.Fatal(err)
			}
			if tt.calls == 1 && !errors.Is(err, tt.err) {
				t.Fatalf("error=%v", err)
			}
		})
	}
}

func TestRetryable(t *testing.T) {
	for _, tt := range []struct {
		err  error
		want bool
	}{
		{&textproto.Error{Code: 451}, true}, {&net.DNSError{IsTemporary: true}, true},
		{&net.DNSError{IsNotFound: true}, false}, {&textproto.Error{Code: 550}, false},
		{&smtp.DeliveryUncertainError{Cause: &net.DNSError{IsTimeout: true}}, false},
		{&tls.CertificateVerificationError{Err: io.EOF}, false}, {nil, false},
	} {
		if got := Retryable(tt.err); got != tt.want {
			t.Fatalf("Retryable(%v)=%v", tt.err, got)
		}
	}
}

func TestDeliverMXUsesTLSFallbackBeforeSubmissionPort(t *testing.T) {
	var calls []string
	err := deliverMX([]string{"mx.example.com"}, func(host, port string) error {
		calls = append(calls, port)
		if port == "465" {
			return nil
		}
		return &net.OpError{Op: "dial", Err: io.EOF}
	})
	if err != nil || !reflect.DeepEqual(calls, []string{"25", "465"}) {
		t.Fatalf("加密路由可用时不应等待提交端口：calls=%v err=%v", calls, err)
	}
}
