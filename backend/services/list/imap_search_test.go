package list

import (
	"fmt"
	"testing"

	"git.pcux.cn/xuans/Smail/config"
	"git.pcux.cn/xuans/Smail/db"
	"git.pcux.cn/xuans/Smail/models"
	"git.pcux.cn/xuans/Smail/utils/context"
	"github.com/emersion/go-imap/v2"
)

func TestSearchEmailsMessageID(t *testing.T) {
	initListTestDB(t)
	config.Instance.Domain = "example.test"
	matching := models.Email{MsgID: "Public-Receipt@example.test", Subject: "matching"}
	foreign := models.Email{MsgID: matching.MsgID, Subject: "foreign"}
	legacy := models.Email{Subject: "legacy"}
	if _, err := db.Instance.Insert(&matching, &foreign, &legacy); err != nil {
		t.Fatal(err)
	}
	inbox := models.UserEmail{UserID: 1, EmailID: matching.Id}
	otherUser := models.UserEmail{UserID: 2, EmailID: foreign.Id}
	legacyInbox := models.UserEmail{UserID: 1, EmailID: legacy.Id}
	if _, err := db.Instance.Insert(&inbox, &otherUser, &legacyInbox); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name  string
		value string
		want  int
	}{
		{name: "full header", value: "<Public-Receipt@example.test>", want: inbox.ID},
		{name: "case insensitive", value: "<PUBLIC-RECEIPT@EXAMPLE.TEST>", want: inbox.ID},
		{name: "substring", value: "receipt@", want: inbox.ID},
		{name: "legacy fallback", value: fmt.Sprintf("<%d@example.test>", legacy.Id), want: legacyInbox.ID},
		{name: "absent value", value: "<missing@example.test>"},
	} {
		t.Run(test.name, func(t *testing.T) {
			criteria := &imap.SearchCriteria{Header: []imap.SearchCriteriaHeaderField{{Key: "mEsSaGe-Id", Value: test.value}}}
			rows, err := SearchEmails(&context.Context{UserID: 1}, "INBOX", criteria)
			if err != nil {
				t.Fatal(err)
			}
			if test.want == 0 {
				if len(rows) != 0 {
					t.Fatalf("unexpected matches: %#v", rows)
				}
			} else if len(rows) != 1 || rows[0].ID != test.want {
				t.Fatalf("matches = %#v, want only relation %d", rows, test.want)
			}
		})
	}
}

func TestMessageIDHeaderPresence(t *testing.T) {
	previous := config.Instance
	config.Instance = nil
	t.Cleanup(func() { config.Instance = previous })
	if matchesHeader(&models.Email{}, "Message-ID", "") {
		t.Fatal("missing header must not match an empty search value")
	}
	if !matchesHeader(&models.Email{MsgID: "stored@example.test"}, "Message-ID", "") {
		t.Fatal("stored header must match an empty search value")
	}
}
