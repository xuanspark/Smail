package app

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"git.pcux.cn/xuans/Smail/services/runtimelog"
	log "github.com/sirupsen/logrus"
)

func TestFatalErrorIsLoggedBeforeRuntimeClose(t *testing.T) {
	for _, panics := range []bool{false, true} {
		root := t.TempDir()
		previousHooks := log.StandardLogger().ReplaceHooks(make(log.LevelHooks))
		writer, err := runtimelog.Start(root)
		if err != nil {
			log.StandardLogger().ReplaceHooks(previousHooks)
			t.Fatal(err)
		}
		err = func() (runErr error) {
			defer finishRuntimeLogs(writer, &runErr)
			if panics {
				panic("fatal-probe")
			}
			return errors.New("fatal-probe")
		}()
		log.StandardLogger().ReplaceHooks(previousHooks)
		if err == nil || !strings.Contains(err.Error(), "fatal-probe") {
			t.Fatalf("fatal error lost: %v", err)
		}
		files, err := filepath.Glob(filepath.Join(root, "log", "runtime-*.log"))
		if err != nil || len(files) != 1 {
			t.Fatalf("missing runtime log: %v", err)
		}
		data, err := os.ReadFile(files[0])
		if err != nil || !strings.Contains(string(data), "fatal-probe") {
			t.Fatal("fatal error was not written before close")
		}
	}
}
