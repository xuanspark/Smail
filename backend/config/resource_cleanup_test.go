package config

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	log "github.com/sirupsen/logrus"
	logtest "github.com/sirupsen/logrus/hooks/test"
)

type cleanupTestResource struct {
	close func() error
}

func (r cleanupTestResource) Close() error { return r.close() }

func TestReleaseConfigLeaseUnlocksBeforeClosing(t *testing.T) {
	var steps []string
	resource := cleanupTestResource{close: func() error {
		steps = append(steps, "close")
		return nil
	}}
	releaseConfigLease(func() { steps = append(steps, "unlock") }, resource)
	if strings.Join(steps, ",") != "unlock,close" {
		t.Fatalf("资源释放顺序不正确: %v", steps)
	}
}

func TestCloseConfigResourceReportsFailureWithoutSensitiveDetails(t *testing.T) {
	previous := log.StandardLogger().ReplaceHooks(make(log.LevelHooks))
	t.Cleanup(func() { log.StandardLogger().ReplaceHooks(previous) })
	hook := logtest.NewGlobal()
	const sensitive = "sensitive-cleanup-value"
	closeCount := 0
	resource := cleanupTestResource{close: func() error {
		closeCount++
		return errors.New(sensitive)
	}}
	closeConfigResource(resource)
	entry := hook.LastEntry()
	if closeCount != 1 || entry == nil || entry.Level != log.ErrorLevel {
		t.Fatal("关闭失败没有记录为错误")
	}
	if strings.Contains(entry.Message+fmt.Sprint(entry.Data), sensitive) {
		t.Fatal("关闭错误日志泄露了资源内部信息")
	}
	hook.Reset()
	closeConfigResource(cleanupTestResource{close: func() error { return nil }})
	if hook.LastEntry() != nil {
		t.Fatal("成功关闭资源不应记录错误")
	}
}
