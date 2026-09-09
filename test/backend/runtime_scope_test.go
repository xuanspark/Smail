package runtimelog

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestScopedRuntimePaginationAndArchive(t *testing.T) {
	w, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	now := time.Now().UTC()
	at := now.Add(-9 * 24 * time.Hour).Truncate(time.Second)
	for i, actor := range []string{"4", "1", "4", "3", "4"} {
		message := fmt.Sprintf(`[error][%s][request-secret][/private/source.go:10]secret raw error @event {"logSchema":1,"actorId":"%s","action":"执行收信规则","result":"failure","error":"private-error"}`, at.Format("2006-01-02 15:04:05"), actor)
		if err := w.WriteAt(at.Add(time.Duration(i)*time.Second), message); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.WriteAt(at.Add(10*time.Second), "unknown secret legacy record"); err != nil {
		t.Fatal(err)
	}
	check := func() {
		t.Helper()
		query := Query{From: at.Add(-time.Second), To: now, Limit: 2, AllowedActors: map[string]bool{"4": true}}
		page, err := w.Query(context.Background(), query)
		if err != nil || len(page.Lines) != 2 || !page.HasMore {
			t.Fatalf("权限过滤后分页错误：%+v %v", page, err)
		}
		for _, line := range page.Lines {
			if strings.Contains(line, "secret") || strings.Contains(line, "private") || strings.Contains(line, "actorId") || !strings.Contains(line, "执行收信规则") {
				t.Fatalf("日志脱敏丢失动作或泄露信息：%s", line)
			}
		}
		query.Offset = 2
		page, err = w.Query(context.Background(), query)
		if err != nil || len(page.Lines) != 1 || page.HasMore {
			t.Fatalf("第二页错误：%+v %v", page, err)
		}
	}
	check()
	if err := w.Maintain(context.Background(), now); err != nil {
		t.Fatal(err)
	}
	files, err := w.Files()
	if err != nil || len(files) != 1 || !strings.HasSuffix(files[0].Name, ".gz") {
		t.Fatalf("归档未生效：%+v %v", files, err)
	}
	check()
}

func TestRuntimePublicEventsAreExplicitlyRedacted(t *testing.T) {
	header := "[2026-09-09T01:00:00Z] [info][2026-09-09 09:00:00]"
	for _, test := range []struct {
		event string
		keep  bool
	}{
		{`{"logSchema":1,"action":"启动主服务","result":"success","object":"Smail","error":"hidden"}`, true},
		{`{"logSchema":1,"action":"任意全局事件","result":"success","object":"Smail"}`, false},
		{`{"logSchema":1,"actorId":"1","action":"启动主服务","object":"Smail"}`, false},
		{`{"logSchema":2,"action":"启动主服务","object":"Smail"}`, false},
	} {
		line, keep := presentScopedLine(header+"hidden @event "+test.event, map[string]bool{"4": true})
		if keep != test.keep || strings.Contains(line, "hidden") {
			t.Fatalf("公共事件边界错误：%s %v", line, keep)
		}
	}
}
