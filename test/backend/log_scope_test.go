package logstore

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func TestLogScopeFiltersBeforePagination(t *testing.T) {
	s := testStore(t)
	if _, err := s.db.Exec(`CREATE TABLE "user" (id INTEGER PRIMARY KEY, role TEXT, is_admin INTEGER)`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`INSERT INTO "user" VALUES (1,'super_admin',1),(2,'admin',1),(3,'admin',1),(4,'user',0),(5,NULL,0),(6,'legacy',1),(7,'user',1)`); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	for _, kind := range []string{"operation", "mail"} {
		for id := 0; id <= 8; id++ {
			if err := s.Write(ctx, Entry{ID: fmt.Sprintf("%s-%d", kind, id), Kind: kind, ActorID: fmt.Sprint(id), CreatedAt: time.Now().Add(time.Duration(-id) * time.Second), Action: "权限验证", Result: "success", Payload: []byte(`{}`)}); err != nil {
				t.Fatal(err)
			}
		}
		for page, actor := range []string{"2", "4", "5", "7"} {
			items, err := s.Query(ctx, Query{Kind: kind, ScopeActorID: "2", IncludeOrdinaryUsers: true, Limit: 1, Offset: page})
			if err != nil || len(items) != 1 || items[0].ActorID != actor {
				t.Fatalf("管理员分页越界：%s %d %+v %v", kind, page, items, err)
			}
		}
		for _, actor := range []string{"0", "1", "3", "6", "8"} {
			items, err := s.Query(ctx, Query{Kind: kind, ScopeActorID: "2", IncludeOrdinaryUsers: true, ActorID: actor})
			if err != nil || len(items) != 0 {
				t.Fatalf("伪造筛选扩大管理员范围：%s %v", actor, err)
			}
		}
		items, err := s.Query(ctx, Query{Kind: kind, ScopeActorID: "4"})
		if err != nil || len(items) != 1 || items[0].ActorID != "4" {
			t.Fatal("普通用户范围不正确")
		}
		items, err = s.Query(ctx, Query{Kind: kind, ScopeActorID: "4", ActorID: "2"})
		if err != nil || len(items) != 0 {
			t.Fatal("普通用户可以伪造操作者")
		}
		items, err = s.Query(ctx, Query{Kind: kind})
		if err != nil || len(items) != 9 {
			t.Fatal("超级管理员丢失系统或历史日志")
		}
	}
}
