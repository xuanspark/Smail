package runtimelog

import (
	"bytes"
	"compress/gzip"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeMigrationFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
}

func TestRuntimeLogFlatDirectoryAndLegacyMigration(t *testing.T) {
	for _, upgrade := range []bool{false, true} {
		t.Run(map[bool]string{false: "fresh", true: "upgrade"}[upgrade], func(t *testing.T) {
			root := t.TempDir()
			directory := filepath.Join(root, "log")
			old := filepath.Join(directory, legacyDirectory)
			now := time.Now().UTC()
			name := prefix + now.Format("2006-01-02") + ".log"
			archivedAt := now.Add(-9 * 24 * time.Hour)
			archived := prefix + archivedAt.Format("2006-01-02") + ".log.gz"
			line := "[" + now.Format(time.RFC3339Nano) + "] 迁移前的运行日志\n"
			var compressed bytes.Buffer
			archive := gzip.NewWriter(&compressed)
			if _, err := archive.Write([]byte("[" + archivedAt.Format(time.RFC3339Nano) + "] 压缩记录\n")); err != nil {
				t.Fatal(err)
			}
			if err := archive.Close(); err != nil {
				t.Fatal(err)
			}
			var modified time.Time
			if upgrade {
				writeMigrationFile(t, filepath.Join(old, name), []byte(line))
				writeMigrationFile(t, filepath.Join(old, archived), compressed.Bytes())
				if err := os.Chtimes(filepath.Join(old, archived), archivedAt, archivedAt); err != nil {
					t.Fatal(err)
				}
				info, err := os.Stat(filepath.Join(old, archived))
				if err != nil {
					t.Fatal(err)
				}
				modified = info.ModTime()
			}
			// 重复启动不重新创建旧目录，迁移后的当前日志仍可追加。
			for attempt := 0; attempt < 2; attempt++ {
				w, err := New(root)
				if err != nil {
					t.Fatal(err)
				}
				defer w.Close()
				w.now = func() time.Time { return now }
				if _, err := os.Lstat(old); !os.IsNotExist(err) {
					t.Fatalf("legacy directory exists: %v", err)
				}
				if upgrade {
					data, err := os.ReadFile(filepath.Join(directory, archived))
					if err != nil || !bytes.Equal(data, compressed.Bytes()) {
						t.Fatalf("archive changed: %v", err)
					}
					info, err := os.Stat(filepath.Join(directory, archived))
					if err != nil || !modified.Equal(info.ModTime()) {
						t.Fatalf("archive time changed: %v", err)
					}
					page, err := w.Read(context.Background(), archived, 0, 100)
					if err != nil || len(page.Lines) != 1 || !strings.Contains(page.Lines[0], "压缩记录") {
						t.Fatalf("archive query: %+v %v", page, err)
					}
				}
				if err := w.WriteAt(now, "迁移后的日志"); err != nil {
					t.Fatal(err)
				}
				if err := w.Maintain(context.Background(), now); err != nil {
					t.Fatal(err)
				}
				files, err := w.Files()
				if err != nil || len(files) != map[bool]int{false: 1, true: 2}[upgrade] {
					t.Fatalf("files: %+v %v", files, err)
				}
				if err := w.Close(); err != nil {
					t.Fatal(err)
				}
				data, err := os.ReadFile(filepath.Join(directory, name))
				if err != nil || (upgrade && !bytes.HasPrefix(data, []byte(line))) {
					t.Fatalf("original log prefix lost: %v", err)
				}
			}
		})
	}
}

func TestRuntimeLogMigrationPreflightPreservesConflicts(t *testing.T) {
	for _, kind := range []string{"different", "identical", "directory", "symlink"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			directory := filepath.Join(root, "log")
			old := filepath.Join(directory, legacyDirectory)
			first, conflict := "runtime-2026-09-07.log", "runtime-2026-09-08.log"
			for _, name := range []string{first, conflict} {
				writeMigrationFile(t, filepath.Join(old, name), []byte("original"))
			}
			target := filepath.Join(directory, conflict)
			switch kind {
			case "directory":
				if err := os.Mkdir(target, 0700); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				if err := os.Symlink(filepath.Join(old, conflict), target); err != nil {
					t.Skipf("symlink unavailable: %v", err)
				}
			default:
				data := "conflicting"
				if kind == "identical" {
					data = "original"
				}
				writeMigrationFile(t, target, []byte(data))
			}
			before, err := os.Lstat(target)
			if err != nil {
				t.Fatal(err)
			}
			if w, err := New(root); err == nil {
				w.Close()
				t.Fatal("conflict accepted")
			}
			for _, name := range []string{first, conflict} {
				data, err := os.ReadFile(filepath.Join(old, name))
				if err != nil || string(data) != "original" {
					t.Fatalf("source changed: %v", err)
				}
			}
			if _, err := os.Lstat(filepath.Join(directory, first)); !os.IsNotExist(err) {
				t.Fatal("preflight moved a file before detecting conflict")
			}
			after, err := os.Lstat(target)
			if err != nil || !os.SameFile(before, after) || before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) {
				t.Fatalf("target changed: %v", err)
			}
		})
	}
}

func TestRuntimeLogMigrationPreservesUnknownAndResumes(t *testing.T) {
	root := t.TempDir()
	directory := filepath.Join(root, "log")
	old := filepath.Join(directory, legacyDirectory)
	// 顶层文件模拟前一次已完成的迁移，损坏归档只搬运，不在迁移阶段改写或删除。
	files := map[string]string{
		filepath.Join(directory, "runtime-2026-09-07.log"): "already migrated",
		filepath.Join(directory, "config-audit-v2.jsonl"):  "historical audit",
		filepath.Join(old, "runtime-2026-09-08.log.gz"):    "damaged archive",
		filepath.Join(old, "runtime-2026-99-99.log"):       "unknown name",
		filepath.Join(old, "manual", "notes.log"):          "manual notes",
	}
	for path, content := range files {
		writeMigrationFile(t, path, []byte(content))
	}
	for attempt := 0; attempt < 2; attempt++ {
		w, err := New(root)
		if err != nil {
			t.Fatal(err)
		}
		if err := w.Close(); err != nil {
			t.Fatal(err)
		}
	}
	for path, content := range files {
		if filepath.Base(path) == "runtime-2026-09-08.log.gz" {
			if _, err := os.Lstat(path); !os.IsNotExist(err) {
				t.Fatal("migrated source remains")
			}
			path = filepath.Join(directory, filepath.Base(path))
		}
		data, err := os.ReadFile(path)
		if err != nil || string(data) != content {
			t.Fatalf("evidence changed %s: %v", path, err)
		}
	}
}

func TestRuntimeLogMigrationRejectsUnsafeSource(t *testing.T) {
	for _, kind := range []string{"legacy-file", "legacy-link", "entry-directory", "entry-link"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			directory := filepath.Join(root, "log")
			old := filepath.Join(directory, legacyDirectory)
			outside := t.TempDir()
			if err := os.MkdirAll(directory, 0700); err != nil {
				t.Fatal(err)
			}
			name := "runtime-2026-09-08.log"
			writeMigrationFile(t, filepath.Join(outside, name), []byte("outside evidence"))
			switch kind {
			case "legacy-file":
				writeMigrationFile(t, old, []byte("not a directory"))
			case "legacy-link":
				if err := os.Symlink(outside, old); err != nil {
					t.Skipf("symlink unavailable: %v", err)
				}
			default:
				if err := os.Mkdir(old, 0700); err != nil {
					t.Fatal(err)
				}
				if kind == "entry-directory" {
					if err := os.Mkdir(filepath.Join(old, name), 0700); err != nil {
						t.Fatal(err)
					}
				} else if err := os.Symlink(filepath.Join(outside, name), filepath.Join(old, name)); err != nil {
					t.Skipf("symlink unavailable: %v", err)
				}
			}
			if w, err := New(root); err == nil {
				w.Close()
				t.Fatal("unsafe source accepted")
			}
			data, err := os.ReadFile(filepath.Join(outside, name))
			if err != nil || string(data) != "outside evidence" {
				t.Fatalf("outside file changed: %v", err)
			}
			if _, err := os.Lstat(filepath.Join(directory, name)); !os.IsNotExist(err) {
				t.Fatal("unsafe source copied")
			}
		})
	}
}

func TestRuntimeLogMigrationCopyRechecksTargetAndSource(t *testing.T) {
	for _, conflict := range []bool{false, true} {
		t.Run(map[bool]string{false: "source-changed", true: "target-created"}[conflict], func(t *testing.T) {
			directory := t.TempDir()
			old := filepath.Join(directory, legacyDirectory)
			name := "runtime-2026-09-08.log"
			writeMigrationFile(t, filepath.Join(old, name), []byte("original"))
			expected, err := os.Stat(filepath.Join(old, name))
			if err != nil {
				t.Fatal(err)
			}
			root, err := os.OpenRoot(directory)
			if err != nil {
				t.Fatal(err)
			}
			defer root.Close()
			legacy, err := root.OpenRoot(legacyDirectory)
			if err != nil {
				t.Fatal(err)
			}
			defer legacy.Close()
			if conflict {
				writeMigrationFile(t, filepath.Join(directory, name), []byte("target evidence"))
			} else {
				writeMigrationFile(t, filepath.Join(old, name), []byte("changed source"))
			}
			if err := moveLegacyLog(legacy, root, name, expected); err == nil {
				t.Fatal("changed migration input accepted")
			}
			data, err := os.ReadFile(filepath.Join(old, name))
			want := map[bool]string{false: "changed source", true: "original"}[conflict]
			if err != nil || string(data) != want {
				t.Fatalf("source lost: %v", err)
			}
			if conflict {
				data, err := os.ReadFile(filepath.Join(directory, name))
				if err != nil || string(data) != "target evidence" {
					t.Fatalf("target overwritten: %v", err)
				}
			} else if _, err := os.Lstat(filepath.Join(directory, name)); !os.IsNotExist(err) {
				t.Fatal("unexpected destination")
			}
		})
	}
}

func TestRuntimeLogMigrationEmptyDirectoryAndSameDayFormats(t *testing.T) {
	for _, empty := range []bool{true, false} {
		t.Run(map[bool]string{true: "empty", false: "same-day-formats"}[empty], func(t *testing.T) {
			root := t.TempDir()
			directory := filepath.Join(root, "log")
			old := filepath.Join(directory, legacyDirectory)
			if err := os.MkdirAll(old, 0700); err != nil {
				t.Fatal(err)
			}
			files := map[string]string{}
			if !empty {
				files["runtime-2026-09-08.log"] = "plain evidence"
				files["runtime-2026-09-08.log.gz"] = "compressed evidence"
				for name, content := range files {
					writeMigrationFile(t, filepath.Join(old, name), []byte(content))
				}
			}
			w, err := New(root)
			if err != nil {
				t.Fatal(err)
			}
			if err := w.Close(); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Lstat(old); !os.IsNotExist(err) {
				t.Fatalf("empty legacy directory remains: %v", err)
			}
			for name, content := range files {
				data, err := os.ReadFile(filepath.Join(directory, name))
				if err != nil || string(data) != content {
					t.Fatalf("same-day evidence changed: %v", err)
				}
			}
		})
	}
}
