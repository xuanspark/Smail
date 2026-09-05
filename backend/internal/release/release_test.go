package release

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"git.pcux.cn/xuans/Smail/internal/buildinfo"
)

func TestNewManifestRejectsMismatchedPair(t *testing.T) {
	mainInfo := buildinfo.Info{Version: "v1", GitHash: "abc", ProtocolVersion: buildinfo.ProtocolVersion}
	adminInfo := buildinfo.Info{Version: "v2", GitHash: "abc", ProtocolVersion: buildinfo.ProtocolVersion}
	if _, err := NewManifest(Pair{Main: mainInfo, Admin: adminInfo}, time.Now()); !errors.Is(err, ErrInvalidPair) {
		t.Fatalf("NewManifest() error = %v", err)
	}
}

func TestInstallPairWritesBothExecutablesAndManifest(t *testing.T) {
	root := t.TempDir()
	mainSource := filepath.Join(root, "main-source")
	adminSource := filepath.Join(root, "admin-source")
	if err := os.WriteFile(mainSource, []byte("main-v2"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(adminSource, []byte("admin-v2"), 0600); err != nil {
		t.Fatal(err)
	}
	mainInfo := buildinfo.Info{Version: "v2", GitHash: "abc", ProtocolVersion: buildinfo.ProtocolVersion}
	target := filepath.Join(root, "bin")
	if err := InstallPair(mainSource, adminSource, target, Pair{Main: mainInfo, Admin: mainInfo}); err != nil {
		t.Fatal(err)
	}
	mainName, adminName := executableNames()
	for _, name := range []string{mainName, adminName, "release.json"} {
		if _, err := os.Stat(filepath.Join(target, name)); err != nil {
			t.Fatalf("missing installed file %s: %v", name, err)
		}
	}
	manifest, err := ReadManifest(target)
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Version != "v2" || manifest.GitHash != "abc" {
		t.Fatalf("unexpected manifest: %+v", manifest)
	}
}

func TestInstallPairRollsBackWhenSecondTargetCannotBeReplaced(t *testing.T) {
	root := t.TempDir()
	mainSource := filepath.Join(root, "main-source")
	adminSource := filepath.Join(root, "admin-source")
	if err := os.WriteFile(mainSource, []byte("main-v2"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(adminSource, []byte("admin-v2"), 0600); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, "bin")
	if err := os.MkdirAll(target, 0750); err != nil {
		t.Fatal(err)
	}
	mainName, adminName := executableNames()
	if err := os.WriteFile(filepath.Join(target, mainName), []byte("main-v1"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, adminName), []byte("admin-v1"), 0600); err != nil {
		t.Fatal(err)
	}
	originalRename := renamePath
	t.Cleanup(func() { renamePath = originalRename })
	renamePath = func(source, destination string) error {
		if filepath.Base(source) == adminName && filepath.Dir(destination) == target && filepath.Ext(destination) == filepath.Ext(adminName) {
			return errors.New("simulated replacement failure")
		}
		return originalRename(source, destination)
	}
	pair := buildinfo.Info{Version: "v2", GitHash: "abc", ProtocolVersion: buildinfo.ProtocolVersion}
	if err := InstallPair(mainSource, adminSource, target, Pair{Main: pair, Admin: pair}); err == nil {
		t.Fatal("expected replacement failure")
	} else if !errors.Is(err, ErrUpgradeRollback) {
		t.Fatalf("error = %v, want rollback marker", err)
	}
	content, err := os.ReadFile(filepath.Join(target, mainName))
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "main-v1" {
		t.Fatalf("main executable was not restored: %q", content)
	}
	adminContent, err := os.ReadFile(filepath.Join(target, adminName))
	if err != nil {
		t.Fatal(err)
	}
	if string(adminContent) != "admin-v1" {
		t.Fatalf("admin executable was not restored: %q", adminContent)
	}
}

func TestValidateManifestRejectsInvalidTimestamp(t *testing.T) {
	info := buildinfo.Info{Version: "v1", GitHash: "abc", ProtocolVersion: buildinfo.ProtocolVersion}
	manifest := Manifest{Version: "v1", GitHash: "abc", ProtocolVersion: buildinfo.ProtocolVersion, CreatedAt: "not-a-timestamp"}
	if err := ValidateManifest(manifest, Pair{Main: info, Admin: info}); !errors.Is(err, ErrInvalidPair) {
		t.Fatalf("ValidateManifest() error = %v, want invalid pair", err)
	}
}

func TestValidateManifestRejectsProtocolMismatch(t *testing.T) {
	info := buildinfo.Info{Version: "v1", GitHash: "abc", ProtocolVersion: buildinfo.ProtocolVersion}
	manifest := Manifest{Version: "v1", GitHash: "abc", ProtocolVersion: buildinfo.ProtocolVersion + 1, CreatedAt: time.Now().UTC().Format(time.RFC3339)}
	if err := ValidateManifest(manifest, Pair{Main: info, Admin: info}); !errors.Is(err, ErrInvalidPair) {
		t.Fatalf("ValidateManifest() error = %v, want invalid pair", err)
	}
}
