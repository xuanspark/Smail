package dbmigration

import (
	"context"
	"errors"
	"testing"
	"time"

	"git.pcux.cn/xuans/Smail/config"
)

// 审查专用：只使用临时数据库，不连接正在运行的服务。
func TestAuditCustomUniqueIndexMustNotDisappear(t *testing.T) {
	root, source := portableFixture(t)
	switched := false
	executor := &PortableExecutor{
		root: root,
		switchConfig: func(context.Context, uint64, config.DatabaseConfig, config.AuditActor) error {
			switched = true
			return nil
		},
	}
	defer executor.Close()
	database, err := executor.connect(context.Background(), source)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`CREATE UNIQUE INDEX audit_unique_user_name ON "user"(name)`); err != nil {
		_ = database.Close()
		t.Fatal(err)
	}
	_ = database.Close()
	plan, err := buildTargetPlan(source, config.DatabaseConfig{Type: config.DatabaseTypeSQLite, LocalRef: "target.db"})
	if err != nil {
		t.Fatal(err)
	}
	err = executor.Run(context.Background(), plan, func(Status) error { return nil })
	if errors.Is(err, ErrSQLUnsupportedObject) && !switched {
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	target, err := executor.connect(context.Background(), plan.targetConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer target.Close()
	var count int
	if err := target.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name='audit_unique_user_name'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	_, duplicateErr := target.Exec(`INSERT INTO "user" (account,name,password) SELECT 'audit-duplicate',name,'hash' FROM "user" LIMIT 1`)
	t.Logf("switched=%v custom_index_count=%d duplicate_insert_error=%v", switched, count, duplicateErr)
	if count == 0 || duplicateErr == nil {
		t.Fatal("迁移成功切库，但自定义唯一索引丢失且重复数据能够写入")
	}
}

func TestAuditRequestDrainMustRespectCancellation(t *testing.T) {
	service := NewService(nil)
	service.requests.RLock()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	done := make(chan struct{})
	go func() {
		release := service.drainRequests(ctx)
		release()
		close(done)
	}()
	select {
	case <-done:
		service.requests.RUnlock()
	case <-time.After(200 * time.Millisecond):
		service.requests.RUnlock()
		<-done
		t.Fatalf("请求上下文已结束，但排空仍等待在途请求释放锁: %v", ctx.Err())
	}
}

func TestAuditMigrationMustPreserveAllocatedMailboxUIDs(t *testing.T) {
	root, source := portableFixture(t)
	switched := false
	executor := &PortableExecutor{
		root: root,
		switchConfig: func(context.Context, uint64, config.DatabaseConfig, config.AuditActor) error {
			switched = true
			return nil
		},
	}
	defer executor.Close()
	database, err := executor.connect(context.Background(), source)
	if err != nil {
		t.Fatal(err)
	}
	// 正常分配两次关系 ID，再模拟清除最后一封邮件；不手工修改序列表。
	var allocated int64
	for range 2 {
		result, err := database.Exec(`INSERT INTO user_email (user_id,email_id) VALUES (40,82)`)
		if err != nil {
			_ = database.Close()
			t.Fatal(err)
		}
		allocated, err = result.LastInsertId()
		if err != nil {
			_ = database.Close()
			t.Fatal(err)
		}
	}
	if _, err := database.Exec(`DELETE FROM user_email WHERE id=?`, allocated); err != nil {
		_ = database.Close()
		t.Fatal(err)
	}
	var sourceSequence int64
	if err := database.QueryRow(`SELECT seq FROM sqlite_sequence WHERE name='user_email'`).Scan(&sourceSequence); err != nil {
		_ = database.Close()
		t.Fatal(err)
	}
	_ = database.Close()
	plan, err := buildTargetPlan(source, config.DatabaseConfig{Type: config.DatabaseTypeSQLite, LocalRef: "target.db"})
	if err != nil {
		t.Fatal(err)
	}
	if err := executor.Run(context.Background(), plan, func(Status) error { return nil }); err != nil {
		t.Fatal(err)
	}
	target, err := executor.connect(context.Background(), plan.targetConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer target.Close()
	var targetSequence int64
	if err := target.QueryRow(`SELECT seq FROM sqlite_sequence WHERE name='user_email'`).Scan(&targetSequence); err != nil {
		t.Fatal(err)
	}
	result, err := target.Exec(`INSERT INTO user_email (user_id,email_id) VALUES (40,82)`)
	if err != nil {
		t.Fatal(err)
	}
	next, err := result.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("switched=%v removed_uid=%d source_sequence=%d target_sequence=%d newly_allocated_uid=%d", switched, allocated, sourceSequence, targetSequence, next)
	if !switched || sourceSequence < allocated {
		t.Fatal("迁移或源库序列前提未成立")
	}
	if next <= allocated {
		t.Fatal("迁移丢失已分配序号的高水位，新关系复用已删除邮件的 UID")
	}
}
