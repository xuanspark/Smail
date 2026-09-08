package runtimelog

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"

	"git.pcux.cn/xuans/Smail/utils/logpolicy"
)

type File struct {
	Name       string `json:"name"`
	Bytes      int64  `json:"bytes"`
	Compressed bool   `json:"compressed"`
}

type Page struct {
	Lines   []string `json:"lines"`
	HasMore bool     `json:"hasMore"`
}

func (w *Writer) Files() ([]File, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return nil, ErrInvalid
	}
	directory, err := w.root.Open(".")
	if err != nil {
		return nil, err
	}
	defer directory.Close()
	entries, err := directory.ReadDir(-1)
	if err != nil {
		return nil, err
	}
	files := make([]File, 0)
	for _, entry := range entries {
		day, ok := fileDay(entry.Name())
		if !ok || !day.Add(24*time.Hour).After(logpolicy.Cutoff(w.now())) {
			continue
		}
		if err := w.regular(entry.Name(), false); err != nil {
			return nil, err
		}
		info, err := entry.Info()
		if err != nil {
			return nil, err
		}
		files = append(files, File{Name: entry.Name(), Bytes: info.Size(), Compressed: strings.HasSuffix(entry.Name(), ".gz")})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Name > files[j].Name })
	return files, nil
}

// Read 仅允许固定名称的受管日志，不接受任意路径；压缩内容在服务端流式读取。
func (w *Writer) Read(ctx context.Context, name string, offset, limit int) (Page, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if _, ok := fileDay(name); !ok || offset < 0 || offset > 1000000 || limit < 1 || limit > 500 || w.closed {
		return Page{}, ErrInvalid
	}
	page := Page{Lines: make([]string, 0, limit)}
	cutoff := logpolicy.Cutoff(w.now())
	position := 0
	const maxResponseBytes = 2 * 1024 * 1024
	bytes := 0
	finished := errors.New("当前页读取完成")
	err := w.scan(ctx, name, func(line []byte, at time.Time) error {
		if at.Before(cutoff) {
			return nil
		}
		position++
		if position <= offset {
			return nil
		}
		if len(page.Lines) >= limit || bytes+len(line) > maxResponseBytes {
			page.HasMore = true
			return finished
		}
		page.Lines = append(page.Lines, string(line))
		bytes += len(line)
		return nil
	})
	if errors.Is(err, finished) {
		err = nil
	}
	return page, err
}
