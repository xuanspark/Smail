package dbmigration

import (
	"context"
	"database/sql"
	"net/http"
	"strings"
	"sync"
	"time"
)

// WritePaused 表示迁移已冻结源数据库，只有读取状态与重启可以继续。
func (s *Service) WritePaused() bool {
	if s == nil {
		return false
	}
	executor, ok := s.runner.(*DatabaseExecutor)
	return ok && executor.portable != nil && executor.portable.paused.Load()
}

// GuardHTTP 等待在途 HTTP 请求及会话提交结束，再独占执行迁移。
// 放在会话中间件外层，避免迁移请求返回时才开始排空旧会话写入。
func GuardHTTP(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		service := Current()
		if service == nil {
			next.ServeHTTP(w, r)
			return
		}
		service.requests.RLock()
		lease := &requestLease{service: service}
		defer lease.release()
		r = r.WithContext(context.WithValue(r.Context(), requestLeaseKey{}, lease))
		if service.WritePaused() && !maintenanceRequest(r) {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.Header().Set("Cache-Control", "no-store")
			w.Header().Set("Retry-After", "30")
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"errorNo":500,"errorMsg":"数据库迁移已暂停业务操作，请立即重启后端","data":"DATABASE_RESTART_REQUIRED"}`))
			return
		}
		next.ServeHTTP(w, r)
	})
}

type requestLeaseKey struct{}

type requestLease struct {
	service   *Service
	exclusive bool
}

func (lease *requestLease) release() {
	if lease == nil || lease.service == nil {
		return
	}
	if lease.exclusive {
		lease.service.requests.Unlock()
	} else {
		lease.service.requests.RUnlock()
	}
}

// 只有权限、请求体与一次性票据均已验证，才排空请求并独占迁移。
// 独占状态由外层中间件持有到会话响应提交完成，不能在控制器返回前释放。
func (s *Service) drainRequests(ctx context.Context) func() {
	if lease, ok := ctx.Value(requestLeaseKey{}).(*requestLease); ok && lease.service == s {
		lease.service.requests.RUnlock()
		if !lease.service.requests.lockContext(ctx) {
			lease.service = nil
			return func() {}
		}
		lease.exclusive = true
		return func() {}
	}
	if !s.requests.lockContext(ctx) {
		return func() {}
	}
	return s.requests.Unlock
}

func maintenanceRequest(r *http.Request) bool {
	path := r.URL.Path
	if r.Method == http.MethodGet {
		if !strings.HasPrefix(path, "/api/") && !strings.HasPrefix(path, "/attachments/") {
			return true
		}
		return path == "/api/ping" || path == "/api/v1/ping" ||
			path == "/api/v1/config/status" || path == "/api/v1/config/base" ||
			(strings.HasPrefix(path, "/api/v1/database/migrations/") && path != "/api/v1/database/migrations/preview")
	}
	return r.Method == http.MethodPost && (path == "/api/user/info" || path == "/api/v1/user/info" ||
		path == "/api/v1/system/restart" || path == "/api/v1/system/restart/preview")
}

type requestGate struct {
	mu      sync.Mutex
	readers int
	writer  bool
	changed chan struct{}
}

func (gate *requestGate) initialize() {
	gate.mu.Lock()
	if gate.changed == nil {
		gate.changed = make(chan struct{})
	}
	gate.mu.Unlock()
}

func (gate *requestGate) RLock() {
	gate.initialize()
	for {
		gate.mu.Lock()
		if !gate.writer {
			gate.readers++
			gate.mu.Unlock()
			return
		}
		changed := gate.changed
		gate.mu.Unlock()
		<-changed
	}
}

func (gate *requestGate) RUnlock() {
	gate.mu.Lock()
	if gate.readers > 0 {
		gate.readers--
	}
	gate.notifyLocked()
	gate.mu.Unlock()
}

func (gate *requestGate) Lock() {
	gate.initialize()
	for {
		gate.mu.Lock()
		if !gate.writer && gate.readers == 0 {
			gate.writer = true
			gate.mu.Unlock()
			return
		}
		changed := gate.changed
		gate.mu.Unlock()
		<-changed
	}
}

func (gate *requestGate) lockContext(ctx context.Context) bool {
	gate.initialize()
	if ctx == nil {
		ctx = context.Background()
	}
	for {
		gate.mu.Lock()
		if !gate.writer && gate.readers == 0 {
			gate.writer = true
			gate.mu.Unlock()
			return true
		}
		changed := gate.changed
		gate.mu.Unlock()
		select {
		case <-ctx.Done():
			return false
		case <-changed:
		}
	}
}

func (gate *requestGate) Unlock() {
	gate.mu.Lock()
	gate.writer = false
	gate.notifyLocked()
	gate.mu.Unlock()
}

func (gate *requestGate) notifyLocked() {
	close(gate.changed)
	gate.changed = make(chan struct{})
}

// WhileWritable 用于会话后台清理，与 HTTP 迁移执行共享排空边界。
func WhileWritable(action func() error) error {
	service := Current()
	if service == nil {
		return action()
	}
	service.requests.RLock()
	defer service.requests.RUnlock()
	if service.WritePaused() {
		return nil
	}
	return action()
}

func (s *Service) Close() {
	if s == nil {
		return
	}
	s.requests.Lock()
	defer s.requests.Unlock()
	if executor, ok := s.runner.(*DatabaseExecutor); ok && executor.portable != nil {
		executor.portable.Close()
	}
}

// WaitForDatabaseDrain 在关闭连接池后确认借出的连接全部归还。
// 等待失败时不能释放迁移源锁或继续内部重启，应终止进程并由外部重新启动。
func WaitForDatabaseDrain(ctx context.Context, database *sql.DB) error {
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for database != nil && database.Stats().InUse > 0 {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
	return nil
}
