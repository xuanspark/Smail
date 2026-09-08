package runtimelog

import (
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"git.pcux.cn/xuans/Smail/utils/logpolicy"
)

func TestRuntimeLogStartupPresentation(t *testing.T) {
	header := "[2026-09-08T14:55:23Z] [info][2026-09-08 22:55:23]"
	source := "git.pcux.cn/xuans/Smail/internal/app/app.go"
	for _, location := range []string{source, "internal/app/app.go", "/srv/Smail/internal/app/app.go", `D:\Desktop\Smail\backend\internal\app\app.go`} {
		for _, request := range []string{"", "[request-42]"} {
			prefix := header + request + "[" + location + ":39]"
			for _, message := range []string{
				"*******************************************************************", "***\tServer Version: dev-test ",
				"***\tGit Commit Hash: abc123 ", "***\tBuild Date: 2026-09-08T14:55:12Z ",
				"***\tBuild GoLang Version: go version go1.26.6 windows/amd64 ",
			} {
				if _, keep := presentLine(prefix + message); keep {
					t.Fatalf("冗余启动记录仍可见：%s", prefix+message)
				}
			}
			for _, tail := range []string{" ", ` \n`, `\r\n`} {
				line := prefix + "***\tServer Start Success" + tail
				if got, keep := presentLine(line); !keep || got != line {
					t.Fatalf("旧启动事件未保留完整原文：%q %v", got, keep)
				}
			}
		}
	}
	for _, line := range []string{
		header + "[" + source + ":39]服务启动，版本：dev-test",
		header + "[" + source + ":39]***\tServer Start Success 后续内容",
		header + "[" + source + ":39]***\t未知启动消息",
		header + "[other/app.go:39]***\tBuild Date: 保留业务消息",
		header + "[other/app.go:39]********",
		header + "[" + source + ":39]Build Date: 不属于横幅",
		strings.Replace(header, "[info]", "[warning]", 1) + "[" + source + ":39]***\tBuild Date: 保留警告",
		strings.Replace(header, "[info]", "[error]", 1) + "[" + source + ":39]********",
		"[2026-09-08T14:55:23Z] 未知格式保留",
	} {
		if got, keep := presentLine(line); !keep || got != line {
			t.Fatalf("误改正常或未知记录：%q => %q %v", line, got, keep)
		}
	}
}

func TestRuntimeLogStartupFilteringBeforePagination(t *testing.T) {
	for _, compressed := range []bool{false, true} {
		for _, onlyNoise := range []bool{false, true} {
			t.Run(fmt.Sprintf("compressed=%v/onlyNoise=%v", compressed, onlyNoise), func(t *testing.T) {
				w, err := New(t.TempDir())
				if err != nil {
					t.Fatal(err)
				}
				defer w.Close()
				now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
				w.now = func() time.Time { return now }
				header := "[2026-09-08T12:00:00Z] [info][2026-09-08 20:00:00][internal/app/app.go:39]"
				noise := header + "********\n"
				original := strings.Repeat(noise, 60)
				var expected []string
				if !onlyNoise {
					original += header + "***\tServer Start Success \n" + noise
					original += header + "***\tServer Version: old \n" + header + "HTTP 服务就绪\n"
					original += header + "***\tBuild Date: old \n" + strings.Replace(header, "[info]", "[warning]", 1) + "需处理的警告\n"
					expected = []string{header + "***\tServer Start Success ", header + "HTTP 服务就绪", strings.Replace(header, "[info]", "[warning]", 1) + "需处理的警告"}
				}
				original += strings.Repeat(noise, 60)
				name := prefix + "2026-09-08.log"
				data := []byte(original)
				if compressed {
					name += ".gz"
					var buffer bytes.Buffer
					archive := gzip.NewWriter(&buffer)
					if _, err := archive.Write(data); err != nil {
						t.Fatal(err)
					}
					if err := archive.Close(); err != nil {
						t.Fatal(err)
					}
					data = buffer.Bytes()
				}
				if err := w.root.WriteFile(name, data, 0600); err != nil {
					t.Fatal(err)
				}
				var received []string
				for offset := 0; ; {
					page, err := w.Read(context.Background(), name, offset, 2)
					if err != nil {
						t.Fatal(err)
					}
					received = append(received, page.Lines...)
					if page.HasMore && len(page.Lines) != 2 {
						t.Fatalf("过滤导致空页或短页：%+v", page)
					}
					if !page.HasMore {
						break
					}
					offset += len(page.Lines)
					if offset > 4 {
						t.Fatal("分页重复返回记录")
					}
				}
				if !reflect.DeepEqual(received, expected) {
					t.Fatalf("分页丢失或重复记录：%q，期望 %q", received, expected)
				}
				page, err := w.Read(context.Background(), name, len(expected), 2)
				if err != nil || len(page.Lines) != 0 || page.HasMore {
					t.Fatalf("尾部隐藏记录触发多余下一页：%+v %v", page, err)
				}
				after, err := w.root.ReadFile(name)
				if err != nil || !bytes.Equal(after, data) {
					t.Fatalf("只读过滤修改了原文件：%v", err)
				}
			})
		}
	}
}

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
	directory := filepath.Join(root, "log")
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

func TestRuntimeLogCurrentWritesDoNotWaitForArchiveRead(t *testing.T) {
	w, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	now := time.Now()
	if err := w.WriteAt(now, "first"); err != nil {
		t.Fatal(err)
	}
	w.archiveMu.Lock()
	done := make(chan error, 1)
	go func() { done <- w.WriteAt(now, "second") }()
	select {
	case err := <-done:
		w.archiveMu.Unlock()
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		w.archiveMu.Unlock()
		<-done
		t.Fatal("current log write waited for archive lock")
	}
}
