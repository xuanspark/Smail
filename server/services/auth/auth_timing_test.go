package auth

import (
	"path/filepath"
	"testing"

	"git.pcux.cn/xuans/Smail/config"
	"git.pcux.cn/xuans/Smail/db"
	"git.pcux.cn/xuans/Smail/models"
)

func TestAuthenticateUsesUniformCredentialErrors(t *testing.T) {
	oldConfig := config.Instance
	oldDB := db.Instance
	testRoot := t.TempDir()
	t.Cleanup(func() {
		config.Instance = oldConfig
		if db.Instance != nil && db.Instance != oldDB {
			_ = db.Instance.Close()
		}
		db.Instance = oldDB
	})
	config.Instance = &config.Config{
		DbType: config.DBTypeSQLite,
		DbDSN:  filepath.Join(testRoot, "uniform-auth.db"),
	}
	if err := db.Init(""); err != nil {
		t.Fatal(err)
	}

	active := models.User{Account: "active-user", Name: "active"}
	if err := SetPassword(&active, "correct-password"); err != nil {
		t.Fatal(err)
	}
	disabled := models.User{Account: "disabled-user", Name: "disabled", Disabled: 1}
	if err := SetPassword(&disabled, "correct-password"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Instance.Insert(&active, &disabled); err != nil {
		t.Fatal(err)
	}

	_, wrongErr := Authenticate("active-user", "wrong-password")
	_, missingErr := Authenticate("missing-user", "wrong-password")
	_, disabledErr := Authenticate("disabled-user", "wrong-password")
	if wrongErr == nil || missingErr == nil || disabledErr == nil {
		t.Fatal("credential failure unexpectedly authenticated")
	}
	if wrongErr.Error() != "invalid credentials" || missingErr.Error() != wrongErr.Error() || disabledErr.Error() != wrongErr.Error() {
		t.Fatalf("credential errors differ: wrong=%q missing=%q disabled=%q", wrongErr, missingErr, disabledErr)
	}
}
