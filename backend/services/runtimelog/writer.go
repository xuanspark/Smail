// Package runtimelog 管理按天写入、七天压缩和三十天滚动清理的运行日志。
package runtimelog

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"git.pcux.cn/xuans/Smail/utils/logpolicy"
	"git.pcux.cn/xuans/Smail/utils/runtimepath"
	log "github.com/sirupsen/logrus"
)

const prefix = "runtime-"
const maxLineBytes = 256 * 1024

var ErrInvalid = errors.New("运行日志文件或查询参数无效")
var active atomic.Pointer[Writer]

type Writer struct {
	archiveMu sync.Mutex
	mu        sync.Mutex
	root      *os.Root
	file      *os.File
	day       string
	now       func() time.Time
	cancel    context.CancelFunc
	done      chan struct{}
	closed    bool
}

func Current() *Writer { return active.Load() }

func New(root string) (*Writer, error) {
	directory, err := runtimepath.SecureDirectory(root, "log")
	if err != nil {
		return nil, err
	}
	directory, err = runtimepath.SecureDirectory(directory, "运行日志")
	if err != nil {
		return nil, err
	}
	handle, err := os.OpenRoot(directory)
	if err != nil {
		return nil, err
	}
	return &Writer{root: handle, now: time.Now}, nil
}

// Start 使用日志钩子，配置重载重设 stdout 时不会丢失文件输出。
func Start(root string) (*Writer, error) {
	w, err := New(root)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	w.cancel, w.done = cancel, make(chan struct{})
	active.Store(w)
	log.AddHook(w)
	go func() {
		defer close(w.done)
		ticker := time.NewTicker(logpolicy.MaintenanceInterval)
		defer ticker.Stop()
		for {
			bounded, stop := context.WithTimeout(ctx, 2*time.Minute)
			err := w.Maintain(bounded, w.now())
			stop()
			if err != nil && ctx.Err() == nil {
				log.WithError(err).Error("运行日志维护失败，源文件保留并在下一轮重试")
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
	return w, nil
}

func (w *Writer) Levels() []log.Level { return log.AllLevels }

func (w *Writer) Fire(entry *log.Entry) error {
	// 复用已有格式器保留请求标识、错误字段和源位置；转义换行保证一条记录一行。
	data, err := entry.Logger.Formatter.Format(entry)
	if err != nil {
		return err
	}
	return w.WriteAt(entry.Time, strings.TrimRight(string(data), "\r\n"))
}

func (w *Writer) WriteAt(at time.Time, message string) error {
	if at.IsZero() {
		return ErrInvalid
	}
	if at.Before(logpolicy.Cutoff(w.now())) {
		return nil
	}
	message = strings.NewReplacer("\r", "\\r", "\n", "\\n", "\x00", "").Replace(message)
	if len(message) > maxLineBytes-128 {
		message = string([]rune(message)[:min(len([]rune(message)), (maxLineBytes-128)/4)]) + "…"
	}
	day := at.UTC().Format("2006-01-02")
	w.mu.Lock()
	// 当天句柄已打开时直接追加，不等待历史归档的读取或压缩。
	if !w.closed && (w.file == nil || w.day != day) {
		w.mu.Unlock()
		w.archiveMu.Lock()
		defer w.archiveMu.Unlock()
		w.mu.Lock()
	}
	defer w.mu.Unlock()
	if w.closed {
		return nil
	}
	if w.file == nil || w.day != day {
		if w.file != nil {
			if err := errors.Join(w.file.Sync(), w.file.Close()); err != nil {
				w.file = nil
				return err
			}
			w.file = nil
		}
		name := prefix + day + ".log"
		if err := w.regular(name, true); err != nil {
			return err
		}
		file, err := w.root.OpenFile(name, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
		if err != nil {
			return err
		}
		w.file, w.day = file, day
	}
	_, err := fmt.Fprintf(w.file, "[%s] %s\n", at.UTC().Format(time.RFC3339Nano), message)
	return err
}

func (w *Writer) regular(name string, allowMissing bool) error {
	if !filepath.IsLocal(name) || filepath.Base(name) != name {
		return ErrInvalid
	}
	info, err := w.root.Lstat(name)
	if allowMissing && os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return ErrInvalid
	}
	return nil
}

func (w *Writer) Close() error {
	if w == nil {
		return nil
	}
	if w.cancel != nil {
		w.cancel()
		<-w.done
	}
	w.archiveMu.Lock()
	defer w.archiveMu.Unlock()
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return nil
	}
	w.closed = true
	active.CompareAndSwap(w, nil)
	var err error
	if w.file != nil {
		err = errors.Join(w.file.Sync(), w.file.Close())
	}
	return errors.Join(err, w.root.Close())
}
