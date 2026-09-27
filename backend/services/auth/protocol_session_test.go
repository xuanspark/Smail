package auth

import (
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"git.pcux.cn/xuans/Smail/config"
	"git.pcux.cn/xuans/Smail/db"
	"git.pcux.cn/xuans/Smail/internal/passwordtest"
	"git.pcux.cn/xuans/Smail/models"
	"git.pcux.cn/xuans/Smail/services/credentials"
	"git.pcux.cn/xuans/Smail/services/notification"
	"xorm.io/xorm"
)

func TestQueuedNotificationAuthorizationTracksCredentialVersion(t *testing.T) {
	oldDB := db.Instance
	engine, err := xorm.NewEngine("sqlite", filepath.Join(t.TempDir(), "notification.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = engine.Close(); db.Instance = oldDB })
	db.Instance = engine
	if err := engine.Sync2(&models.User{}, &models.NotificationGrant{}); err != nil {
		t.Fatal(err)
	}
	user := models.User{Account: "sender", Password: "test-hash", Role: "user", SessionVersion: 1, SecurityVersion: 1}
	if _, err := engine.Insert(&user); err != nil {
		t.Fatal(err)
	}
	code := "sn_" + strings.Repeat("A", 43)
	item := models.NotificationGrant{
		UserID: user.ID, Enabled: 1, CodeVersion: 2, CodeHash: queuedDigest(code),
		SessionVersion: user.SessionVersion, SecurityVersion: user.SecurityVersion,
	}
	if _, err := engine.Insert(&item); err != nil {
		t.Fatal(err)
	}
	grant, err := notification.Authenticate(engine, user, code)
	if err != nil {
		t.Fatal(err)
	}
	session := &ProtocolSession{user: user, notification: grant}
	snapshot, err := session.QueueAuthorization()
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Kind != "notification" || snapshot.ID != user.ID || snapshot.Version != item.CodeVersion ||
		snapshot.Credential == code || snapshot.Credential == item.CodeHash {
		t.Fatal("notification snapshot is not bound to the original credential")
	}
	if valid, err := snapshot.Check(engine, user); err != nil || !valid {
		t.Fatalf("notification queue authorization = %v, %v", valid, err)
	}
	for _, change := range []string{"disable", "version", "hash", "session", "security"} {
		t.Run(change, func(t *testing.T) {
			updated := item
			switch change {
			case "disable":
				updated.Enabled = 0
			case "version":
				updated.CodeVersion++
			case "hash":
				updated.CodeHash = queuedDigest("replacement")
			case "session":
				updated.SessionVersion++
			case "security":
				updated.SecurityVersion++
			}
			if _, err := engine.ID(user.ID).Cols("enabled", "code_version", "code_hash", "session_version", "security_version").Update(&updated); err != nil {
				t.Fatal(err)
			}
			if valid, err := snapshot.Check(engine, user); err != nil || valid {
				t.Fatalf("stale notification queue authorization = %v, %v", valid, err)
			}
		})
	}
}

func TestProtocolSessionRechecksUserAndCredential(t *testing.T) {
	for _, kind := range []string{"protocol", "client"} {
		for _, change := range []string{"revoke", "disabled", "password", "session_version", "security_version", "role", "deleted", "database_failure"} {
			t.Run(kind+"/"+change, func(t *testing.T) {
				oldDB, oldConfig := db.Instance, config.Instance
				engine, err := xorm.NewEngine("sqlite", filepath.Join(t.TempDir(), "session.db"))
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = engine.Close(); db.Instance, config.Instance = oldDB, oldConfig })
				db.Instance = engine
				config.Instance = &config.Config{Domain: "example.com"}
				if err := engine.Sync2(&models.User{}, &models.UserEmail{}, &models.ClientCredential{}, &models.ProtocolCredential{}); err != nil {
					t.Fatal(err)
				}
				user := models.User{Account: "session-user", Password: "test-hash", Role: "user", SessionVersion: 1, SecurityVersion: 1}
				if _, err := engine.Insert(&user); err != nil {
					t.Fatal(err)
				}
				address, now := "session-user@example.com", time.Now().UTC()
				if _, err := engine.Insert(&models.UserEmail{UserID: user.ID, Address: address, AddressCanonical: &address, VerifiedAt: &now}); err != nil {
					t.Fatal(err)
				}
				service, err := credentials.NewService(engine)
				if err != nil {
					t.Fatal(err)
				}
				var code string
				var clientID int
				if kind == "protocol" {
					if err := service.SetProtocolEnabled(user.ID, "smtp", true); err != nil {
						t.Fatal(err)
					}
					code, err = service.GenerateProtocolCode(user.ID, "smtp")
				} else {
					var item *credentials.ClientCredentialItem
					code, item, err = service.CreateClientCredential(user.ID, "会话验收")
					if item != nil {
						clientID = item.ID
					}
				}
				if err != nil {
					t.Fatal(err)
				}
				_, session, err := AuthenticateProtocolSession("smtp", "192.0.2.91:1234", address, code)
				if err != nil {
					t.Fatal(err)
				}
				queued, err := session.QueueAuthorization()
				if err != nil {
					t.Fatal(err)
				}
				if valid, err := queued.Check(engine, user); err != nil || !valid {
					t.Fatalf("new queued authorization = %v, %v", valid, err)
				}
				if queued.Credential == code || queued.Identity == user.Password || queued.Kind == "" {
					t.Fatal("queued authorization contains reusable credentials or has no kind")
				}
				// 命令复核不占用密码哈希容量，也不累加登录失败计数。
				release := passwordtest.Hold(t)
				if err := session.Check(); err != nil {
					t.Fatal(err)
				}
				release()
				switch change {
				case "revoke":
					if kind == "protocol" {
						err = service.RevokeProtocolCode(user.ID, "smtp")
					} else {
						err = service.RevokeClientCredential(user.ID, clientID)
					}
				case "disabled":
					_, err = engine.ID(user.ID).Cols("disabled").Update(&models.User{Disabled: 1})
				case "password":
					_, err = engine.ID(user.ID).Cols("password").Update(&models.User{Password: "new-hash"})
				case "session_version":
					_, err = engine.ID(user.ID).Incr("session_version", 1).Update(&models.User{})
				case "security_version":
					_, err = engine.ID(user.ID).Incr("security_version", 1).Update(&models.User{})
				case "role":
					_, err = engine.ID(user.ID).Cols("role").Update(&models.User{Role: "admin"})
				case "deleted":
					_, err = engine.ID(user.ID).Delete(&models.User{})
				case "database_failure":
					err = engine.Close()
				}
				if err != nil {
					t.Fatal(err)
				}
				current := user
				if change != "database_failure" {
					current = models.User{}
					if _, err := engine.ID(user.ID).Get(&current); err != nil {
						t.Fatal(err)
					}
				}
				valid, checkErr := queued.Check(engine, current)
				if valid || (change == "database_failure") != (checkErr != nil) {
					t.Fatalf("stale queued authorization = %v, %v", valid, checkErr)
				}
				var wait sync.WaitGroup
				for range 4 {
					wait.Add(1)
					go func() {
						defer wait.Done()
						if session.Check() == nil {
							t.Error("旧会话仍可访问数据")
						}
					}()
				}
				wait.Wait()
				if change == "disabled" {
					if _, err := engine.ID(user.ID).Cols("disabled").Update(&models.User{}); err != nil {
						t.Fatal(err)
					}
					if session.Check() == nil {
						t.Fatal("被拒绝的连接不应恢复授权")
					}
				}
			})
		}
	}
}
