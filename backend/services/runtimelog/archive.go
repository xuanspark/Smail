package runtimelog

import (
	"bufio"
	"compress/gzip"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"strings"
	"time"

	"git.pcux.cn/xuans/Smail/utils/logpolicy"
)

func fileDay(name string) (time.Time, bool) {
	base := strings.TrimSuffix(name, ".gz")
	if !strings.HasPrefix(base, prefix) || !strings.HasSuffix(base, ".log") {
		return time.Time{}, false
	}
	value := strings.TrimSuffix(strings.TrimPrefix(base, prefix), ".log")
	day, err := time.Parse("2006-01-02", value)
	return day, err == nil && (name == prefix+value+".log" || name == prefix+value+".log.gz")
}

func lineTime(line []byte) (time.Time, error) {
	if len(line) < 3 || line[0] != '[' {
		return time.Time{}, ErrInvalid
	}
	end := strings.IndexByte(string(line), ']')
	if end < 0 {
		return time.Time{}, ErrInvalid
	}
	at, err := time.Parse(time.RFC3339Nano, string(line[1:end]))
	if err != nil {
		return time.Time{}, ErrInvalid
	}
	return at, nil
}

// scan 严格解析每条记录及压缩校验和，损坏归档不得触发删除源文件。
func (w *Writer) scan(ctx context.Context, name string, consume func([]byte, time.Time) error) error {
	return w.scanSize(ctx, name, -1, consume)
}

func (w *Writer) scanSize(ctx context.Context, name string, size int64, consume func([]byte, time.Time) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := w.regular(name, false); err != nil {
		return err
	}
	file, err := w.root.Open(name)
	if err != nil {
		return err
	}
	defer file.Close()
	var reader io.Reader = file
	if size >= 0 {
		reader = io.LimitReader(reader, size)
	}
	if strings.HasSuffix(name, ".gz") {
		archive, err := gzip.NewReader(reader)
		if err != nil {
			return err
		}
		defer archive.Close()
		reader = archive
	}
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 4096), maxLineBytes)
	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return err
		}
		at, err := lineTime(scanner.Bytes())
		if err != nil {
			return err
		}
		if err := consume(scanner.Bytes(), at); err != nil {
			return err
		}
	}
	return scanner.Err()
}

func (w *Writer) Maintain(ctx context.Context, now time.Time) error {
	if now.IsZero() {
		return ErrInvalid
	}
	w.archiveMu.Lock()
	defer w.archiveMu.Unlock()
	if w.closed {
		return nil
	}
	cutoff := logpolicy.Cutoff(now)
	w.mu.Lock()
	openDay, _ := time.Parse("2006-01-02", w.day)
	// 仅关闭本轮要处理的历史句柄，当天写入不与历史文件扫描共用长时间锁。
	if w.file != nil && (!openDay.After(cutoff) || !openDay.Add(24*time.Hour).After(now.Add(-logpolicy.CompressionAge))) {
		err := errors.Join(w.file.Sync(), w.file.Close())
		w.file = nil
		if err != nil {
			w.mu.Unlock()
			return err
		}
	}
	w.mu.Unlock()
	directory, err := w.root.Open(".")
	if err != nil {
		return err
	}
	entries, err := directory.ReadDir(-1)
	closeErr := directory.Close()
	if err != nil || closeErr != nil {
		return errors.Join(err, closeErr)
	}
	var failures []error
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		day, ok := fileDay(entry.Name())
		if !ok {
			continue
		}
		compress := !strings.HasSuffix(entry.Name(), ".gz") && !day.Add(24*time.Hour).After(now.Add(-logpolicy.CompressionAge))
		if !day.After(cutoff) || compress {
			if err := w.transform(ctx, entry.Name(), day, cutoff, compress); err != nil {
				failures = append(failures, err)
			}
		}
	}
	return errors.Join(failures...)
}

// transform 流式过滤边界日；额外内存不随一天的日志量增长。
func (w *Writer) transform(ctx context.Context, source string, day, cutoff time.Time, compress bool) error {
	target := source
	if compress {
		target += ".gz"
	}
	var token [16]byte
	if _, err := rand.Read(token[:]); err != nil {
		return err
	}
	temporary := ".pending-" + hex.EncodeToString(token[:]) + ".log"
	if strings.HasSuffix(target, ".gz") {
		temporary += ".gz"
	}
	file, err := w.root.OpenFile(temporary, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close(); _ = w.root.Remove(temporary) }()
	var output io.Writer = file
	var archive *gzip.Writer
	if strings.HasSuffix(target, ".gz") {
		archive = gzip.NewWriter(file)
		output = archive
	}
	hash := sha256.New()
	output = io.MultiWriter(output, hash)
	kept, removed := 0, 0
	err = w.scan(ctx, source, func(line []byte, at time.Time) error {
		// 文件名和记录日期不一致时保留现场，不按文件名误删。
		if at.UTC().Format("2006-01-02") != day.Format("2006-01-02") {
			return ErrInvalid
		}
		if at.Before(cutoff) {
			removed++
			return nil
		}
		kept++
		_, err := output.Write(append(append([]byte(nil), line...), '\n'))
		return err
	})
	if archive != nil {
		err = errors.Join(err, archive.Close())
	}
	if err = errors.Join(err, file.Sync(), file.Close()); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if removed == 0 && !compress {
		return nil
	}
	if kept == 0 {
		if err := w.regular(source, false); err != nil {
			return err
		}
		return w.root.Remove(source)
	}
	verified := sha256.New()
	if err := w.scan(ctx, temporary, func(line []byte, _ time.Time) error {
		_, err := verified.Write(append(append([]byte(nil), line...), '\n'))
		return err
	}); err != nil {
		return err
	}
	if !strings.EqualFold(hex.EncodeToString(hash.Sum(nil)), hex.EncodeToString(verified.Sum(nil))) {
		return ErrInvalid
	}
	if source != target {
		if _, err := w.root.Lstat(target); err == nil {
			// 压缩成功但删除前中断时，只有目标与本轮内容完全一致才允许补做删除。
			existing := sha256.New()
			if err := w.scan(ctx, target, func(line []byte, _ time.Time) error {
				_, err := existing.Write(append(append([]byte(nil), line...), '\n'))
				return err
			}); err != nil {
				return err
			}
			if hex.EncodeToString(existing.Sum(nil)) != hex.EncodeToString(hash.Sum(nil)) {
				return ErrInvalid
			}
			return w.root.Remove(source)
		} else if !os.IsNotExist(err) {
			return err
		}
	}
	if err := w.regular(target, true); err != nil {
		return err
	}
	if err := w.root.Rename(temporary, target); err != nil {
		return err
	}
	if source != target {
		return w.root.Remove(source)
	}
	return nil
}
