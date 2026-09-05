package res_init

import (
	"context"
	"errors"
	"testing"
	"time"

	"git.pcux.cn/xuans/Smail/services/adminreset"
)

func TestAdminResetDisabledForDevelopment(t *testing.T) {
	for _, tt := range []struct {
		name        string
		development bool
		args        []string
		want        bool
	}{
		{name: "production"},
		{name: "legacy development", development: true, want: true},
		{name: "explicit development override", args: []string{developmentDisableAdminResetFlag}, want: true},
		{name: "unrelated flag", args: []string{"--debug"}},
		{name: "partial flag rejected", args: []string{developmentDisableAdminResetFlag + "=false"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := adminResetDisabledForDevelopment(tt.development, tt.args); got != tt.want {
				t.Fatalf("disabled = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDevelopmentDoesNotOpenAdminResetControl(t *testing.T) {
	t.Cleanup(stopAdminResetControl)
	listen := func(string, []uint32) (adminreset.ControlServer, error) {
		t.Fatal("development must not open an administrator reset endpoint")
		return nil, nil
	}
	if err := startAdminResetControlWithListener(true, adminreset.DefaultControlPath, listen); err != nil {
		t.Fatal(err)
	}
	if adminResetControl != nil || adminResetServeErrors != nil || adminResetCancel != nil {
		t.Fatal("development must not leave an active reset channel")
	}
}

func TestProductionAdminResetListenFailureIsFatal(t *testing.T) {
	want := errors.New("access denied")
	listen := func(path string, allowedUIDs []uint32) (adminreset.ControlServer, error) {
		if path != adminreset.DefaultControlPath || len(allowedUIDs) != 1 || allowedUIDs[0] != 0 {
			t.Fatal("production reset permissions changed")
		}
		return nil, want
	}
	if err := startAdminResetControlWithListener(false, adminreset.DefaultControlPath, listen); !errors.Is(err, want) {
		t.Fatalf("error = %v, want %v", err, want)
	}
}

func TestProductionAdminResetRejectsUnavailableControl(t *testing.T) {
	listen := func(string, []uint32) (adminreset.ControlServer, error) { return nil, nil }
	for _, path := range []string{"", adminreset.DefaultControlPath} {
		if err := startAdminResetControlWithListener(false, path, listen); err == nil {
			t.Fatal("production must reject unavailable reset control")
		}
	}
}

type failingAdminResetControl struct {
	err error
}

func (s *failingAdminResetControl) Serve(context.Context, adminreset.ResetHandler, *adminreset.SessionGuard) error {
	return s.err
}

func (s *failingAdminResetControl) Close() error { return nil }

func TestProductionAdminResetServeFailureIsReported(t *testing.T) {
	t.Cleanup(stopAdminResetControl)
	want := errors.New("accept denied")
	listen := func(string, []uint32) (adminreset.ControlServer, error) {
		return &failingAdminResetControl{err: want}, nil
	}
	if err := startAdminResetControlWithListener(false, adminreset.DefaultControlPath, listen); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-adminResetServeErrors:
		if !errors.Is(err, want) {
			t.Fatalf("error = %v, want %v", err, want)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("production must report reset control serve failures")
	}
}
