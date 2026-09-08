package runtimelog

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"git.pcux.cn/xuans/Smail/utils/logpolicy"
)

func TestRuntimeLogCompressionAndExactRetention(t *testing.T) {
	root := t.TempDir()
	w, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	cutoff := logpolicy.Cutoff(now)
	w.now = func() time.Time { return cutoff }
	for _, at := range []time.Time{cutoff.Add(-time.Hour), cutoff, cutoff.Add(time.Hour), now.Add(-8 * 24 * time.Hour), now} {
		if err := w.WriteAt(at, "sample\nsecond line"); err != nil {
			t.Fatal(err)
		}
	}
	w.now = func() time.Time { return now }
	ctx := context.Background()
	if err := w.Maintain(ctx, now); err != nil {
		t.Fatal(err)
	}
	files, err := w.Files()
	if err != nil || len(files) != 3 {
		t.Fatalf("files: %+v %v", files, err)
	}
	name := prefix + cutoff.Format("2006-01-02") + ".log.gz"
	page, err := w.Read(ctx, name, 0, 100)
	if err != nil || len(page.Lines) != 2 {
		t.Fatalf("boundary: %+v %v", page, err)
	}
	if !strings.Contains(page.Lines[0], `\n`) {
		t.Fatal("multiline event split")
	}
	// 压缩后仍须逐条裁剪，不能按整个归档日期删除有效记录。
	now = now.Add(30 * time.Minute)
	if err := w.Maintain(ctx, now); err != nil {
		t.Fatal(err)
	}
	page, err = w.Read(ctx, name, 0, 100)
	if err != nil || len(page.Lines) != 1 {
		t.Fatalf("compressed boundary: %+v %v", page, err)
	}
	now = now.Add(24 * time.Hour)
	if err := w.Maintain(ctx, now); err != nil {
		t.Fatal(err)
	}
	if _, err := w.root.Stat(name); !os.IsNotExist(err) {
		t.Fatalf("expired archive still exists: %v", err)
	}
	if _, err := w.Read(ctx, "../config/config.json", 0, 100); err == nil {
		t.Fatal("path traversal accepted")
	}
	if err := w.Maintain(ctx, now); err != nil {
		t.Fatal(err)
	}
}

func TestRuntimeLogCorruptionAndUnknownFilesArePreserved(t *testing.T) {
	root := t.TempDir()
	w, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	now := time.Now().UTC()
	w.now = func() time.Time { return now }
	directory := filepath.Join(root, "log", "运行日志")
	name := prefix + now.Add(-40*24*time.Hour).Format("2006-01-02") + ".log.gz"
	if err := os.WriteFile(filepath.Join(directory, name), []byte("broken"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "manual.log"), []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := w.Maintain(context.Background(), now); err == nil {
		t.Fatal("corruption not reported")
	}
	for _, file := range []string{name, "manual.log"} {
		if _, err := os.Stat(filepath.Join(directory, file)); err != nil {
			t.Fatal("unmanaged or damaged evidence deleted")
		}
	}
}
