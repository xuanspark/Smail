package credentials

import (
	"encoding/json"
	"testing"

	"git.pcux.cn/xuans/Smail/models"
)

func TestProtocolGrantTracksCredentialChanges(t *testing.T) {
	for _, change := range []string{"revoke", "rotate", "disable_enable", "delete", "hash", "version", "database"} {
		t.Run(change, func(t *testing.T) {
			s := newFixture(t)
			if err := s.SetProtocolEnabled(1, ProtocolSMTP, true); err != nil {
				t.Fatal(err)
			}
			code, err := s.GenerateProtocolCode(1, ProtocolSMTP)
			if err != nil {
				t.Fatal(err)
			}
			grant, err := s.CheckClientOrProtocolGrant(1, ProtocolSMTP, code)
			if err != nil || !grant.Valid(s.engine) {
				t.Fatalf("授权快照无效：%v", err)
			}
			data, err := json.Marshal(grant)
			if err != nil || string(data) != "{}" {
				t.Fatal("授权快照泄漏内部字段")
			}
			switch change {
			case "revoke":
				err = s.RevokeProtocolCode(1, ProtocolSMTP)
			case "rotate":
				_, err = s.GenerateProtocolCode(1, ProtocolSMTP)
			case "disable_enable":
				err = s.SetProtocolEnabled(1, ProtocolSMTP, false)
				if err == nil {
					err = s.SetProtocolEnabled(1, ProtocolSMTP, true)
				}
			case "delete":
				_, err = s.engine.Where("user_id = ?", 1).Delete(&models.ProtocolCredential{})
			case "hash":
				_, err = s.engine.Where("user_id = ?", 1).Cols("code_hash").Update(&models.ProtocolCredential{CodeHash: "replacement"})
			case "version":
				_, err = s.engine.Where("user_id = ?", 1).Incr("code_version", 1).Update(&models.ProtocolCredential{})
			case "database":
				if grant.Valid(newFixture(t).engine) {
					t.Fatal("数据库切换后沿用旧授权")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if grant.Valid(s.engine) {
				t.Fatal("失效的凭据快照仍然有效")
			}
		})
	}
}

func TestClientGrantRevocationPreservesOtherClients(t *testing.T) {
	s := newFixture(t)
	first, item, err := s.CreateClientCredential(1, "被撤销客户端")
	if err != nil {
		t.Fatal(err)
	}
	second, _, err := s.CreateClientCredential(1, "保留客户端")
	if err != nil {
		t.Fatal(err)
	}
	for _, protocol := range SupportedProtocols() {
		grant, err := s.CheckClientOrProtocolGrant(1, protocol, first)
		if err != nil || !grant.Valid(s.engine) {
			t.Fatalf("客户端授权失败：%v", err)
		}
	}
	firstGrant, err := s.CheckClientOrProtocolGrant(1, ProtocolIMAP, first)
	if err != nil {
		t.Fatal(err)
	}
	secondGrant, err := s.CheckClientOrProtocolGrant(1, ProtocolIMAP, second)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.RevokeClientCredential(1, item.ID); err != nil {
		t.Fatal(err)
	}
	if firstGrant.Valid(s.engine) || !secondGrant.Valid(s.engine) {
		t.Fatal("客户端撤销范围不正确")
	}
}
