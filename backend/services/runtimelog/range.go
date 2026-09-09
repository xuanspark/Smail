package runtimelog

import (
	"container/heap"
	"context"
	"errors"
	"sort"
	"time"

	"git.pcux.cn/xuans/Smail/utils/logpolicy"
)

type Query struct {
	// nil 表示完整管理视图；非 nil 时先按可信归属过滤并脱敏，再进行分页。
	// 映射值为 true 表示本人；false 表示代管普通用户，事件发生时也必须是普通用户。
	AllowedActors map[string]bool
	File          string
	From          time.Time
	To            time.Time
	Offset        int
	Limit         int
}

type lineRef struct {
	at       time.Time
	file     int
	position int
	bytes    int
}

// 索引只保存时间与行位置，不把历史日志全文装入内存。
type recentLines []lineRef

func (h recentLines) Len() int { return len(h) }
func (h recentLines) Less(i, j int) bool {
	if !h[i].at.Equal(h[j].at) {
		return h[i].at.Before(h[j].at)
	}
	if h[i].file != h[j].file {
		return h[i].file > h[j].file
	}
	return h[i].position < h[j].position
}
func (h recentLines) Swap(i, j int)   { h[i], h[j] = h[j], h[i] }
func (h *recentLines) Push(value any) { *h = append(*h, value.(lineRef)) }
func (h *recentLines) Pop() any {
	last := len(*h) - 1
	value := (*h)[last]
	*h = (*h)[:last]
	return value
}

// Query 跨日流式扫描并按真实时间倒序，兼容压缩文件和同一文件中的乱序写入。
// 额外内存由最大偏移限制的行索引和两兆响应组成，不随日志文件体积增长。
func (w *Writer) Query(ctx context.Context, query Query) (Page, error) {
	if query.File != "" {
		if _, ok := fileDay(query.File); !ok {
			return Page{}, ErrInvalid
		}
	}
	if query.Offset < 0 || query.Offset > 100000 || query.Limit < 1 || query.Limit > 500 {
		return Page{}, ErrInvalid
	}
	if query.To.IsZero() {
		query.To = w.now()
	}
	if query.From.IsZero() {
		query.From = query.To.Add(-24 * time.Hour)
	}
	if query.From.After(query.To) {
		return Page{}, ErrInvalid
	}
	query.From = maxTime(query.From, logpolicy.Cutoff(w.now()))
	// 等待归档时也响应取消，避免查询超时后继续占用 HTTP 工作协程。
	for !w.archiveMu.TryLock() {
		select {
		case <-ctx.Done():
			return Page{}, ctx.Err()
		case <-time.After(10 * time.Millisecond):
		}
	}
	defer w.archiveMu.Unlock()
	if err := ctx.Err(); err != nil {
		return Page{}, err
	}
	// 在短写锁内冻结文件长度；两次扫描不会读到新追加的半条记录。
	w.mu.Lock()
	files, err := w.files()
	w.mu.Unlock()
	if err != nil {
		return Page{}, err
	}
	page := Page{Lines: make([]string, 0, query.Limit)}
	if query.From.After(query.To) {
		return page, nil
	}
	capacity := query.Offset + query.Limit + 1
	refs := make(recentLines, 0, capacity)
	for index, file := range files {
		if query.File != "" && query.File != file.Name {
			continue
		}
		day, _ := fileDay(file.Name)
		if day.After(query.To) || !day.Add(24*time.Hour).After(query.From) {
			continue
		}
		position := 0
		err := w.scanSize(ctx, file.Name, file.Bytes, func(line []byte, at time.Time) error {
			position++
			if !at.UTC().Truncate(24 * time.Hour).Equal(day) {
				return ErrInvalid
			}
			if at.Before(query.From) || at.After(query.To) {
				return nil
			}
			visible, keep := presentScopedLine(string(line), query.AllowedActors)
			if !keep {
				return nil
			}
			candidate := lineRef{at: at, file: index, position: position, bytes: len(visible)}
			if len(refs) < capacity {
				heap.Push(&refs, candidate)
			} else if (recentLines{refs[0], candidate}).Less(0, 1) {
				refs[0] = candidate
				heap.Fix(&refs, 0)
			}
			return nil
		})
		if err != nil {
			return Page{}, err
		}
	}
	sort.Sort(sort.Reverse(refs))
	if query.Offset >= len(refs) {
		return page, nil
	}
	refs = refs[query.Offset:]
	page.HasMore = len(refs) > query.Limit
	refs = refs[:min(len(refs), query.Limit)]
	bytes := 0
	for index, ref := range refs {
		if bytes+ref.bytes > 2*1024*1024 {
			refs = refs[:index]
			page.HasMore = true
			break
		}
		bytes += ref.bytes
	}
	page.Lines = make([]string, len(refs))
	selected := make(map[int]map[int]int)
	for index, ref := range refs {
		if selected[ref.file] == nil {
			selected[ref.file] = make(map[int]int)
		}
		selected[ref.file][ref.position] = index
	}
	finished := errors.New("本页内容读取完成")
	for fileIndex, positions := range selected {
		position, remaining := 0, len(positions)
		file := files[fileIndex]
		err := w.scanSize(ctx, file.Name, file.Bytes, func(line []byte, at time.Time) error {
			position++
			index, ok := positions[position]
			if !ok {
				return nil
			}
			visible, keep := presentScopedLine(string(line), query.AllowedActors)
			if !keep || !at.Equal(refs[index].at) || len(visible) != refs[index].bytes {
				return ErrInvalid
			}
			page.Lines[index] = visible
			remaining--
			if remaining == 0 {
				return finished
			}
			return nil
		})
		if err != nil && !errors.Is(err, finished) {
			return Page{}, err
		}
		if remaining != 0 {
			return Page{}, ErrInvalid
		}
	}
	return page, nil
}

func maxTime(left, right time.Time) time.Time {
	if left.After(right) {
		return left
	}
	return right
}
