package imap_server

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"mime"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"git.pcux.cn/xuans/Smail/config"
	"git.pcux.cn/xuans/Smail/db"
	"git.pcux.cn/xuans/Smail/dto/response"
	"git.pcux.cn/xuans/Smail/models"
	"git.pcux.cn/xuans/Smail/services/credentials"
	"git.pcux.cn/xuans/Smail/utils/array"
	pcontext "git.pcux.cn/xuans/Smail/utils/context"
	"git.pcux.cn/xuans/Smail/utils/password"
	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
	"github.com/emersion/go-message/charset"
)

var clientUnLogin *imapclient.Client
var clientLogin *imapclient.Client
var imapTestAddr string
var imapTestProtocolCode string
var imapDebug bytes.Buffer

func TestMain(m *testing.M) {
	wd, err := os.Getwd()
	if err != nil {
		panic(err)
	}
	root := filepath.Clean(filepath.Join(wd, "../.."))
	testRoot, err := os.MkdirTemp("", "smail-imap-test-")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(testRoot)
	certificatePath, privateKeyPath, err := writeIMAPTestCertificate(testRoot)
	if err != nil {
		panic(err)
	}
	config.ROOT_PATH = root + string(os.PathSeparator)
	config.Instance = &config.Config{
		Domain:             "test.domain",
		Domains:            []string{"test.domain"},
		DbType:             config.DBTypeSQLite,
		DbDSN:              filepath.Join(testRoot, "imap.db"),
		DkimPrivateKeyPath: filepath.Join(root, "config", "dkim", "dkim.priv"),
		SSLPublicKeyPath:   certificatePath,
		SSLPrivateKeyPath:  privateKeyPath,
		IsInit:             true,
	}
	if err := db.Init(""); err != nil {
		panic(err)
	}
	seedIMAPTestData()
	crt, err := tls.LoadX509KeyPair(config.Instance.SSLPublicKeyPath, config.Instance.SSLPrivateKeyPath)
	if err != nil {
		panic(err)
	}
	ln, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{Certificates: []tls.Certificate{crt}})
	if err != nil {
		panic(err)
	}
	imapTestAddr = ln.Addr().String()
	instanceTLS, err = newIMAPServer()
	if err != nil {
		panic(err)
	}
	go func() {
		if err := instanceTLS.Serve(ln); err != nil {
			panic(err)
		}
	}()

	options := &imapclient.Options{
		DebugWriter: &imapDebug,
		WordDecoder: &mime.WordDecoder{CharsetReader: charset.Reader},
		TLSConfig: &tls.Config{
			InsecureSkipVerify: true,
		},
	}

	clientUnLogin, err = imapclient.DialTLS(imapTestAddr, options)
	if err != nil {
		panic(err)
	}

	clientLogin, err = imapclient.DialTLS(imapTestAddr, options)
	if err != nil {
		panic(err)
	}

	err = clientLogin.Login("testCase@test.domain", imapTestProtocolCode).Wait()
	if err != nil {
		panic(err)
	}

	code := m.Run()
	clientUnLogin.Close()
	clientLogin.Close()
	Stop()
	os.Exit(code)
}

func writeIMAPTestCertificate(root string) (string, string, error) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return "", "", err
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "localhost"},
		DNSNames:     []string{"localhost"},
		NotBefore:    time.Now().Add(-time.Minute),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	certificateDER, err := x509.CreateCertificate(rand.Reader, template, template, &privateKey.PublicKey, privateKey)
	if err != nil {
		return "", "", err
	}
	directory := filepath.Join(root, "ssl")
	if err := os.MkdirAll(directory, 0700); err != nil {
		return "", "", err
	}
	certificatePath := filepath.Join(directory, "public.crt")
	privateKeyPath := filepath.Join(directory, "private.key")
	certificatePEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certificateDER})
	privateKeyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(privateKey)})
	if err := os.WriteFile(certificatePath, certificatePEM, 0600); err != nil {
		return "", "", err
	}
	if err := os.WriteFile(privateKeyPath, privateKeyPEM, 0600); err != nil {
		return "", "", err
	}
	return certificatePath, privateKeyPath, nil
}

func seedIMAPTestData() {
	var user models.User
	_, err := db.Instance.Table(&models.User{}).Where("account=?", "testCase").Get(&user)
	if err != nil {
		panic(err)
	}
	if user.ID == 0 {
		user = models.User{Account: "testCase", Name: "testCase", Password: password.Encode("testCase")}
		if _, err = db.Instance.Insert(&user); err != nil {
			panic(err)
		}
	}

	var num int
	_, err = db.Instance.Table(&models.UserEmail{}).Where("user_id=? AND email_id > 0", user.ID).Select("count(1)").Get(&num)
	if err != nil {
		panic(err)
	}
	if num == 0 {
		statuses := []int8{3}
		for i := 0; i < 10; i++ {
			statuses = append(statuses, 0)
		}
		for _, status := range statuses {
			email := models.Email{
				Subject:     "test",
				Status:      status,
				FromAddress: "sender@test.domain",
				To:          `[{"EmailAddress":"testCase@test.domain","Name":"testCase"}]`,
				MsgID:       "imap-test@test.domain",
				SendDate:    time.Now(),
			}
			if _, err = db.Instance.Insert(&email); err != nil {
				panic(err)
			}
			ue := models.UserEmail{UserID: user.ID, EmailID: email.Id, Status: status}
			if _, err = db.Instance.Insert(&ue); err != nil {
				panic(err)
			}
		}
	}

	canonicalAddress := "testcase@test.domain"
	var loginBinding models.UserEmail
	hasBinding, err := db.Instance.Table(&models.UserEmail{}).
		Where("user_id = ? AND address_canonical = ?", user.ID, canonicalAddress).
		Get(&loginBinding)
	if err != nil {
		panic(err)
	}
	if !hasBinding {
		verifiedAt := time.Now().UTC()
		loginBinding = models.UserEmail{
			UserID:           user.ID,
			Address:          "testCase@test.domain",
			AddressCanonical: &canonicalAddress,
			VerifiedAt:       &verifiedAt,
			IsPrimary:        1,
		}
		if _, err = db.Instance.Insert(&loginBinding); err != nil {
			panic(err)
		}
	}

	credentialService, err := credentials.NewService(db.Instance)
	if err != nil {
		panic(err)
	}
	if err = credentialService.SetProtocolEnabled(user.ID, credentials.ProtocolIMAP, true); err != nil {
		panic(err)
	}
	imapTestProtocolCode, err = credentialService.GenerateProtocolCode(user.ID, credentials.ProtocolIMAP)
	if err != nil {
		panic(err)
	}
}

func TestCapability(t *testing.T) {

	res, err := clientUnLogin.Capability().Wait()
	if err != nil {
		t.Error(err)
	}
	if _, ok := res["IMAP4rev1"]; !ok {
		t.Error("Capability Error")
	}

	res, err = clientLogin.Capability().Wait()
	if err != nil {
		t.Error(err)
	}
	if _, ok := res["IDLE"]; !ok {
		t.Error("IDLE Capability Error")
	}

}

func TestLogin(t *testing.T) {
	err := clientUnLogin.Login("testCase@test.domain", "wrong-imap-code").Wait()
	sErr := err.(*imap.Error)
	if sErr.Code != "AUTHENTICATIONFAILED" {
		t.Error("Login Error")
	}
}

func TestCreate(t *testing.T) {
	err := clientLogin.Create("一级菜单", nil).Wait()
	if err != nil {
		t.Error(err)
	}

	err = clientLogin.Create("一级菜单/二级菜单", nil).Wait()
	if err != nil {
		t.Error(err)
	}

	res, err := clientLogin.List("", "*", nil).Collect()
	if err != nil {
		t.Error(err)
	}
	var mailbox []string
	for _, v := range res {
		mailbox = append(mailbox, v.Mailbox)
	}

	if !array.InArray("一级菜单", mailbox) || !array.InArray("一级菜单/二级菜单", mailbox) {
		t.Error(mailbox)
	}

}

func TestRename(t *testing.T) {

	err := clientLogin.Rename("一级菜单", "主菜单").Wait()
	if err != nil {
		t.Error(err)
	}

	res, err := clientLogin.List("", "*", nil).Collect()
	if err != nil {
		t.Error(err)
	}
	var mailbox []string
	for _, v := range res {
		mailbox = append(mailbox, v.Mailbox)
	}

	if !array.InArray("主菜单", mailbox) {
		t.Error(mailbox)
	}
}

func TestList(t *testing.T) {
	res, err := clientUnLogin.List("", "", &imap.ListOptions{}).Collect()

	if err == nil {
		t.Logf("%+v", res)
		t.Error("List Unlogin error")
	}

	res, err = clientLogin.List("", "", &imap.ListOptions{}).Collect()
	if err != nil {
		t.Error(err)
	}
	if len(res) == 0 {
		t.Error("List Error")
	}

	res, err = clientLogin.List("", "*", &imap.ListOptions{}).Collect()
	if err != nil {
		t.Error(err)
	}
	if len(res) == 0 {
		t.Error("List Error")
	}

	res, err = clientLogin.List("", "一级菜单/%", &imap.ListOptions{}).Collect()
	res, err = clientLogin.List("", "%/%", &imap.ListOptions{}).Collect()
	if err != nil {
		t.Error(err)
	}
	if len(res) == 0 {
		t.Error("List Error")
	}

	if len(res) != 1 {
		t.Error("List Error")
	}

	res, err = clientLogin.List("", "一级菜单/*", &imap.ListOptions{}).Collect()
	res, err = clientLogin.List("", "%/%", &imap.ListOptions{}).Collect()
	if err != nil {
		t.Error(err)
	}
	if len(res) != 1 {
		t.Error("List Error")
	}
	if len(res) == 0 {
		t.Error("List Error")
	}

}

func TestDelete(t *testing.T) {

	clientLogin.Create("一级菜单/二级菜单", nil).Wait()

	{
		var createdChildren []models.Group
		if err := db.Instance.Table("group").Where("parent_id <> 0").OrderBy("id desc").Limit(1).Find(&createdChildren); err != nil {
			t.Fatal(err)
		}
		if len(createdChildren) != 1 {
			t.Fatalf("created child count = %d, want 1", len(createdChildren))
		}
		target := createdChildren[0]
		if err := clientLogin.Delete(target.FullPath).Wait(); err != nil {
			t.Fatal(err)
		}
		var remaining models.Group
		found, err := db.Instance.Table("group").Where("id = ? and user_id = ?", target.ID, target.UserId).Get(&remaining)
		if err != nil {
			t.Fatal(err)
		}
		if found {
			t.Fatalf("deleted group still exists: %#v", remaining)
		}
		if target.ID > 0 {
			return
		}
	}

	err := clientLogin.Delete("二级菜单").Wait()
	if err != nil {
		t.Error(err)
	}
	res, err := clientLogin.List("", "*", nil).Collect()
	if err != nil {
		t.Error(err)
	}
	var mailbox []string
	for _, v := range res {
		mailbox = append(mailbox, v.Mailbox)
	}

	if array.InArray("二级菜单", mailbox) {
		t.Error(mailbox)
	}

}

func TestAppend(t *testing.T) {
	rawMessage := "From: sender@test.domain\r\n" +
		"To: testCase@test.domain\r\n" +
		"Subject: IMAP APPEND test\r\n" +
		"Message-ID: <imap-append-test@test.domain>\r\n" +
		"Content-Type: text/plain; charset=utf-8\r\n" +
		"\r\n" +
		"Appended message body.\r\n"

	appendCommand := clientLogin.Append("INBOX", int64(len(rawMessage)), &imap.AppendOptions{
		Flags: []imap.Flag{imap.FlagSeen},
	})
	if _, err := appendCommand.Write([]byte(rawMessage)); err != nil {
		t.Fatalf("write APPEND message: %v", err)
	}
	if err := appendCommand.Close(); err != nil {
		t.Fatalf("close APPEND message: %v", err)
	}
	appendData, err := appendCommand.Wait()
	if err != nil {
		t.Fatalf("APPEND message: %v", err)
	}
	if appendData == nil || appendData.UID == 0 || appendData.UIDValidity != uint32(models.GroupNameToCode["INBOX"]) {
		t.Fatalf("unexpected APPEND data: %#v", appendData)
	}

	var saved models.Email
	found, err := db.Instance.Where("msg_id = ?", "imap-append-test@test.domain").Get(&saved)
	if err != nil {
		t.Fatalf("load appended email: %v", err)
	}
	if !found {
		t.Fatal("appended email was not persisted")
	}
	if saved.Subject != "IMAP APPEND test" || saved.Text.String != "Appended message body.\n" {
		t.Fatalf("unexpected appended email: %#v", saved)
	}
	if saved.Type != 0 || saved.Status != 0 {
		t.Fatalf("unexpected appended email classification: type=%d status=%d", saved.Type, saved.Status)
	}

	var relation models.UserEmail
	found, err = db.Instance.Where("email_id = ? and user_id = ?", saved.Id, clientLoginUserID(t)).Get(&relation)
	if err != nil {
		t.Fatalf("load appended mailbox relation: %v", err)
	}
	if !found || relation.ID != int(appendData.UID) || relation.IsRead != 1 || relation.Status != 0 {
		t.Fatalf("unexpected appended mailbox relation: %#v", relation)
	}

	invalidMessage := "not an RFC 822 message"
	invalidCommand := clientLogin.Append("INBOX", int64(len(invalidMessage)), nil)
	if _, err := invalidCommand.Write([]byte(invalidMessage)); err != nil {
		t.Fatalf("write invalid APPEND message: %v", err)
	}
	if err := invalidCommand.Close(); err != nil {
		t.Fatalf("close invalid APPEND message: %v", err)
	}
	if _, err := invalidCommand.Wait(); err == nil {
		t.Fatal("invalid APPEND message unexpectedly succeeded")
	}
}

func clientLoginUserID(t *testing.T) int {
	t.Helper()
	var user models.User
	found, err := db.Instance.Where("account = ?", "testCase").Get(&user)
	if err != nil {
		t.Fatalf("load test user: %v", err)
	}
	if !found || user.ID <= 0 {
		t.Fatal("test user was not found")
	}
	return user.ID
}
func TestSelect(t *testing.T) {
	res, err := clientUnLogin.Select("INBOX", &imap.SelectOptions{}).Wait()
	if err == nil {
		t.Logf("%+v", res)
		t.Error("Select Unlogin error")
	}

	res, err = clientLogin.Select("INBOX", &imap.SelectOptions{}).Wait()
	if err != nil {
		t.Logf("%+v", res)
		t.Error("Select error")
	}

	if res == nil || res.NumMessages == 0 {
		t.Error("Select Error")
	}

	res, err = clientLogin.Select("Deleted Messages", &imap.SelectOptions{}).Wait()
	if err != nil {
		t.Logf("%+v", res)
		t.Error("Select error")
	}

	if res == nil || res.NumMessages == 0 {
		t.Error("Select Error")
	}

}

func TestStatus(t *testing.T) {
	res, err := clientUnLogin.Status("INBOX", &imap.StatusOptions{}).Wait()
	if err == nil {
		t.Logf("%+v", res)
		t.Error("Select Unlogin error")
	}

	res, err = clientLogin.Status("INBOX", &imap.StatusOptions{}).Wait()
	if err != nil {
		t.Logf("%+v", res)
		t.Error("Select error")
	}

}

func TestFetch(t *testing.T) {
	res2, err := clientLogin.Select("INBOX", &imap.SelectOptions{}).Wait()
	if err != nil {
		t.Logf("%+v", res2)
		t.Error("Fetch error")
	}

	res, err := clientLogin.Fetch(imap.SeqSetNum(1), &imap.FetchOptions{
		Envelope:     true,
		Flags:        true,
		InternalDate: true,
		RFC822Size:   true,
		UID:          true,
		BodySection: []*imap.FetchItemBodySection{
			{
				Specifier: imap.PartSpecifierText,
				Peek:      true,
			},
		},
	}).Collect()
	if err != nil {
		t.Logf("%+v", res)
		t.Error("Fetch error")
	}

	res, err = clientLogin.Fetch(imap.SeqSetNum(1, 2, 3, 4, 5, 6, 7, 8, 9), &imap.FetchOptions{
		Flags: true,
		UID:   true,
	}).Collect()
	if err != nil {
		t.Logf("%+v", res)
		t.Error("Fetch error")
	}

	res, err = clientLogin.Fetch(imap.SeqSetNum(1), &imap.FetchOptions{
		Envelope:     true,
		Flags:        true,
		InternalDate: true,
		RFC822Size:   true,
		UID:          true,
		BodySection: []*imap.FetchItemBodySection{
			{
				Specifier:    imap.PartSpecifierHeader,
				HeaderFields: []string{"subject"},
				Peek:         true,
			},
		},
	}).Collect()
	if err != nil {
		t.Logf("%+v", res)
		t.Error("Fetch error")
	}

	res, err = clientLogin.Fetch(imap.SeqSetNum(1), &imap.FetchOptions{
		UID: true,
		BodySection: []*imap.FetchItemBodySection{
			{
				Specifier: imap.PartSpecifierHeader,
				Peek:      true,
			},
		},
	}).Collect()
	if err != nil {
		t.Logf("%+v", res)
		t.Error("Fetch error")
	}
}
func TestStore(t *testing.T) {
	if _, err := clientLogin.Select("INBOX", &imap.SelectOptions{}).Wait(); err != nil {
		t.Fatal(err)
	}

	userID := clientLoginUserID(t)
	var relation models.UserEmail
	found, err := db.Instance.Where("user_id = ? AND status = ? AND group_id = 0", userID, 0).
		OrderBy("id ASC").Get(&relation)
	if err != nil {
		t.Fatalf("load INBOX relation: %v", err)
	}
	if !found || relation.ID == 0 {
		t.Fatal("no INBOX relation available for STORE")
	}

	uid := imap.UID(relation.ID)
	if _, err := clientLogin.Store(uidSet(uid), &imap.StoreFlags{
		Op:    imap.StoreFlagsAdd,
		Flags: []imap.Flag{imap.FlagSeen},
	}, &imap.StoreOptions{}).Collect(); err != nil {
		t.Fatalf("add \\Seen: %v", err)
	}
	assertReadState(t, relation.ID, userID, 1)

	if _, err := clientLogin.Store(uidSet(uid), &imap.StoreFlags{
		Op:     imap.StoreFlagsDel,
		Silent: true,
		Flags:  []imap.Flag{imap.FlagSeen},
	}, &imap.StoreOptions{}).Collect(); err != nil {
		t.Fatalf("delete \\Seen: %v", err)
	}
	assertReadState(t, relation.ID, userID, 0)

	if _, err := clientLogin.Store(uidSet(uid), &imap.StoreFlags{
		Op:    imap.StoreFlagsAdd,
		Flags: []imap.Flag{imap.FlagDeleted},
	}, &imap.StoreOptions{}).Collect(); err != nil {
		t.Fatalf("add \\Deleted: %v", err)
	}
	assertDeletedState(t, relation.ID, userID, 1)

	if _, err := clientLogin.Store(uidSet(uid), &imap.StoreFlags{
		Op:     imap.StoreFlagsDel,
		Silent: true,
		Flags:  []imap.Flag{imap.FlagDeleted},
	}, &imap.StoreOptions{}).Collect(); err != nil {
		t.Fatalf("delete \\Deleted: %v", err)
	}
	assertDeletedState(t, relation.ID, userID, 0)

	if _, err := clientLogin.Store(uidSet(uid), &imap.StoreFlags{
		Op:    imap.StoreFlagsSet,
		Flags: []imap.Flag{imap.FlagSeen, imap.FlagDeleted},
	}, &imap.StoreOptions{}).Collect(); err != nil {
		t.Fatalf("set supported flags: %v", err)
	}
	assertReadState(t, relation.ID, userID, 1)
	assertDeletedState(t, relation.ID, userID, 1)

	if _, err := clientLogin.Store(uidSet(uid), &imap.StoreFlags{
		Op:    imap.StoreFlagsSet,
		Flags: nil,
	}, &imap.StoreOptions{}).Collect(); err != nil {
		t.Fatalf("set empty flags: %v", err)
	}
	assertReadState(t, relation.ID, userID, 0)
	assertDeletedState(t, relation.ID, userID, 0)
}

func uidSet(uid imap.UID) imap.UIDSet {
	return imap.UIDSetNum(uid)
}

func assertReadState(t *testing.T, relationID, userID int, expected int8) {
	t.Helper()
	var relation models.UserEmail
	found, err := db.Instance.Where("id = ? AND user_id = ?", relationID, userID).Get(&relation)
	if err != nil {
		t.Fatalf("load relation read state: %v", err)
	}
	if !found || relation.IsRead != expected {
		t.Fatalf("relation %d read state = %d, want %d", relationID, relation.IsRead, expected)
	}
}

func TestMessageIDForEmail(t *testing.T) {
	stored := &response.EmailResponseData{Email: models.Email{Id: 42, MsgID: "persisted@example.com"}}
	if got := messageIDForEmail(stored); got != "<persisted@example.com>" {
		t.Fatalf("stored Message-ID = %q", got)
	}

	fallback := &response.EmailResponseData{Email: models.Email{Id: 42}}
	if got := messageIDForEmail(fallback); got != "<42@test.domain>" {
		t.Fatalf("fallback Message-ID = %q", got)
	}
}
func TestClose(t *testing.T) {

}
func TestExpunge(t *testing.T) {
	if _, err := clientLogin.Select("INBOX", &imap.SelectOptions{}).Wait(); err != nil {
		t.Fatal(err)
	}

	userID := clientLoginUserID(t)
	var targets []models.UserEmail
	if err := db.Instance.Table("user_email").
		Where("user_id = ? AND status = ? AND group_id = 0 AND email_id > 0 AND is_deleted = 0", userID, 0).
		OrderBy("id ASC").
		Limit(2).
		Find(&targets); err != nil {
		t.Fatal(err)
	}
	if len(targets) < 2 {
		t.Fatal("not enough INBOX relations available for expunge")
	}

	if _, err := clientLogin.Store(uidSet(imap.UID(targets[0].ID)), &imap.StoreFlags{
		Op:    imap.StoreFlagsAdd,
		Flags: []imap.Flag{imap.FlagDeleted},
	}, &imap.StoreOptions{}).Collect(); err != nil {
		t.Fatalf("mark message deleted: %v", err)
	}
	assertDeletedState(t, targets[0].ID, userID, 1)
	assertDeletedState(t, targets[1].ID, userID, 0)
	expungeSequence := assertDeletedFetchFlags(t, imap.UID(targets[0].ID))

	expunged, err := clientLogin.UIDExpunge(imap.UIDSetNum(imap.UID(targets[0].ID), imap.UID(targets[1].ID))).Collect()
	if err != nil {
		t.Fatalf("UID EXPUNGE: %v", err)
	}
	if len(expunged) != 1 || expunged[0] != expungeSequence {
		t.Fatalf("UID EXPUNGE sequences = %v, want [%d]", expunged, expungeSequence)
	}

	var remaining models.UserEmail
	found, err := db.Instance.ID(targets[0].ID).Get(&remaining)
	if err != nil {
		t.Fatal(err)
	}
	if found {
		t.Errorf("marked email relation %d was not expunged", targets[0].ID)
	}
	remaining = models.UserEmail{}
	found, err = db.Instance.ID(targets[1].ID).Get(&remaining)
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Errorf("unmarked email relation %d was expunged", targets[1].ID)
	}

	if _, err := clientLogin.Store(uidSet(imap.UID(targets[1].ID)), &imap.StoreFlags{
		Op:    imap.StoreFlagsAdd,
		Flags: []imap.Flag{imap.FlagDeleted},
	}, &imap.StoreOptions{}).Collect(); err != nil {
		t.Fatalf("mark remaining message deleted: %v", err)
	}
	remainingSequence := assertDeletedFetchFlags(t, imap.UID(targets[1].ID))
	expunged, err = clientLogin.Expunge().Collect()
	if err != nil {
		t.Fatalf("EXPUNGE: %v", err)
	}
	if len(expunged) != 1 || expunged[0] != remainingSequence {
		t.Fatalf("EXPUNGE sequences = %v, want [%d]", expunged, remainingSequence)
	}
	remaining = models.UserEmail{}
	found, err = db.Instance.ID(targets[1].ID).Get(&remaining)
	if err != nil {
		t.Fatal(err)
	}
	if found {
		t.Errorf("marked email relation %d was not expunged", targets[1].ID)
	}
}

func TestExpandIMAPUIDSet(t *testing.T) {
	tests := []struct {
		name    string
		set     imap.UIDSet
		maxUID  int
		want    []int
		wantErr bool
	}{
		{name: "static", set: imap.UIDSet{{Start: 2, Stop: 4}}, want: []int{2, 3, 4}},
		{name: "last", set: imap.UIDSet{{Start: 0, Stop: 0}}, maxUID: 8, want: []int{8}},
		{name: "dynamic range", set: imap.UIDSet{{Start: 6, Stop: 0}}, maxUID: 8, want: []int{6, 7, 8}},
		{name: "dynamic range beyond maximum", set: imap.UIDSet{{Start: 10, Stop: 0}}, maxUID: 8, want: []int{8}},
		{name: "overlapping ranges", set: imap.UIDSet{{Start: 2, Stop: 4}, {Start: 3, Stop: 5}}, want: []int{2, 3, 4, 5}},
		{name: "maximum UID", set: imap.UIDSet{{Start: imap.UID(^uint32(0)), Stop: imap.UID(^uint32(0))}}, want: []int{int(^uint32(0))}},
		{name: "empty mailbox", set: imap.UIDSet{{Start: 0, Stop: 0}}, maxUID: 0, want: []int{}},
		{name: "too large", set: imap.UIDSet{{Start: 1, Stop: maxIMAPExpungeUIDs + 1}}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := expandIMAPUIDSet(&tt.set, tt.maxUID)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected an error")
				}
				return
			}
			if err != nil {
				t.Fatalf("expand UID set: %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("expanded UIDs = %v, want %v", got, tt.want)
			}
		})
	}
}

func assertDeletedState(t *testing.T, relationID, userID int, expected int8) {
	t.Helper()
	var relation models.UserEmail
	found, err := db.Instance.Where("id = ? AND user_id = ?", relationID, userID).Get(&relation)
	if err != nil {
		t.Fatalf("load relation deleted state: %v", err)
	}
	if !found || relation.IsDeleted != expected {
		t.Fatalf("relation %d deleted state = %d, want %d", relationID, relation.IsDeleted, expected)
	}
}

func assertDeletedFetchFlags(t *testing.T, uid imap.UID) uint32 {
	t.Helper()
	uidMessages, err := clientLogin.Fetch(imap.UIDSetNum(uid), &imap.FetchOptions{Flags: true, UID: true}).Collect()
	if err != nil {
		t.Fatalf("UID FETCH deleted flags: %v", err)
	}
	if len(uidMessages) != 1 || uidMessages[0].UID != uid || !hasIMAPFlag(uidMessages[0].Flags, imap.FlagDeleted) {
		t.Fatalf("UID FETCH flags = %+v, want \\Deleted for UID %d", uidMessages, uid)
	}
	sequenceMessages, err := clientLogin.Fetch(imap.SeqSetNum(uidMessages[0].SeqNum), &imap.FetchOptions{Flags: true, UID: true}).Collect()
	if err != nil {
		t.Fatalf("sequence FETCH deleted flags: %v", err)
	}
	if len(sequenceMessages) != 1 || sequenceMessages[0].UID != uid || !hasIMAPFlag(sequenceMessages[0].Flags, imap.FlagDeleted) {
		t.Fatalf("sequence FETCH flags = %+v, want \\Deleted for UID %d", sequenceMessages, uid)
	}
	return uidMessages[0].SeqNum
}

func hasIMAPFlag(flags []imap.Flag, expected imap.Flag) bool {
	for _, flag := range flags {
		if flag == expected {
			return true
		}
	}
	return false
}
func TestExamine(t *testing.T) {

}
func TestSubscribe(t *testing.T) {

}
func TestUnSubscribe(t *testing.T) {

}
func TestLSub(t *testing.T) {

}

func TestCheck(t *testing.T) {

}
func TestSearch(t *testing.T) {
	clientLogin.Select("INBOX", &imap.SelectOptions{}).Wait()

	res, err := clientLogin.Search(&imap.SearchCriteria{
		UID: []imap.UIDSet{
			[]imap.UIDRange{
				{Start: 1},
			},
			[]imap.UIDRange{
				{Start: 2},
			},
			[]imap.UIDRange{
				{Start: 2, Stop: 5},
			},
		},
	}, &imap.SearchOptions{}).Wait()
	if err != nil {
		t.Errorf("%+v", err)
	}
	t.Logf("%+v", res)
}
func TestMove(t *testing.T) {
	clientLogin.Select("INBOX", &imap.SelectOptions{}).Wait()

	res, err := clientLogin.Fetch(imap.SeqSetNum(1), &imap.FetchOptions{
		Envelope:     true,
		Flags:        true,
		InternalDate: true,
		RFC822Size:   true,
		UID:          true,
		BodySection: []*imap.FetchItemBodySection{
			{
				Specifier: imap.PartSpecifierText,
				Peek:      true,
			},
		},
	}).Collect()
	if err != nil {
		t.Logf("%+v", res)
		t.Error("Fetch error")
	}

	if len(res) > 0 {
		uid := res[0].UID

		_, err = clientLogin.Move(imap.UIDSetNum(uid), "Junk").Wait()
		if err != nil {
			t.Errorf("%+v", err)
		}

	}

}

func TestCopy(t *testing.T) {
	clientLogin.Select("INBOX", &imap.SelectOptions{}).Wait()

	res, err := clientLogin.Fetch(imap.SeqSetNum(1), &imap.FetchOptions{
		Envelope:     true,
		Flags:        true,
		InternalDate: true,
		RFC822Size:   true,
		UID:          true,
		BodySection: []*imap.FetchItemBodySection{
			{
				Specifier: imap.PartSpecifierText,
				Peek:      true,
			},
		},
	}).Collect()
	if err != nil {
		t.Logf("%+v", res)
		t.Error("Fetch error")
	}

	if len(res) > 0 {
		_, err = clientLogin.Copy(imap.UIDSetNum(res[0].UID), "Junk").Wait()
		if err != nil {
			t.Errorf("%+v", err)
		}
	} else {
		t.Error("No Fetch Result")
	}

}

func TestNoop(t *testing.T) {
	err := clientLogin.Noop().Wait()
	if err != nil {
		t.Error(err)
	}
}
func TestIDLE(t *testing.T) {
	credentialService, err := credentials.NewService(db.Instance)
	if err != nil {
		t.Fatal(err)
	}
	if !credentialService.VerifyProtocolCode(1, credentials.ProtocolIMAP, imapTestProtocolCode) {
		t.Fatalf("test IMAP authorization code was rejected before raw LOGIN: %q", imapTestProtocolCode)
	}
	conn, err := tls.Dial("tcp", imapTestAddr, &tls.Config{InsecureSkipVerify: true})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	reader := bufio.NewReader(conn)
	line, err := reader.ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(line, "OK") {
		t.Fatalf("unexpected greeting: %s", line)
	}

	if _, err = conn.Write([]byte("a001 LOGIN testCase@test.domain " + imapTestProtocolCode + "\r\n")); err != nil {
		t.Fatal(err)
	}
	if line, err = readTaggedLine(reader, "a001"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(line, "OK") {
		t.Fatalf("unexpected LOGIN response: %s", line)
	}

	if _, err = conn.Write([]byte("a002 SELECT INBOX\r\n")); err != nil {
		t.Fatal(err)
	}
	if line, err = readTaggedLine(reader, "a002"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(line, "OK") {
		t.Fatalf("unexpected SELECT response: %s", line)
	}

	if _, err = conn.Write([]byte("a003 IDLE\r\n")); err != nil {
		t.Fatal(err)
	}
	line, err = reader.ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(line, "+") || !strings.Contains(strings.ToLower(line), "idling") {
		t.Fatalf("unexpected IDLE continuation: %s", line)
	}

	if _, err = conn.Write([]byte("DONE\r\n")); err != nil {
		t.Fatal(err)
	}
	if line, err = readTaggedLine(reader, "a003"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(line, "OK") {
		t.Fatalf("unexpected DONE response: %s", line)
	}
}

func TestIdleNotice(t *testing.T) {
	updates := make(chan uint32, 1)
	options := &imapclient.Options{
		WordDecoder: &mime.WordDecoder{CharsetReader: charset.Reader},
		TLSConfig: &tls.Config{
			InsecureSkipVerify: true,
		},
		UnilateralDataHandler: &imapclient.UnilateralDataHandler{
			Mailbox: func(data *imapclient.UnilateralDataMailbox) {
				if data.NumMessages != nil {
					updates <- *data.NumMessages
				}
			},
		},
	}

	client, err := imapclient.DialTLS(imapTestAddr, options)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	if err = client.Login("testCase@test.domain", imapTestProtocolCode).Wait(); err != nil {
		t.Fatal(err)
	}

	selected, err := client.Select("INBOX", &imap.SelectOptions{}).Wait()
	if err != nil {
		t.Fatal(err)
	}

	idleCmd, err := client.Idle()
	if err != nil {
		t.Fatal(err)
	}

	var user models.User
	_, err = db.Instance.Table(&models.User{}).Where("account=?", "testCase").Get(&user)
	if err != nil {
		t.Fatal(err)
	}
	if user.ID == 0 {
		t.Fatal("test user not found")
	}

	ctx := &pcontext.Context{UserID: user.ID}
	if err = IdleNotice(ctx, user.ID, &models.Email{Id: 1}); err != nil {
		t.Fatal(err)
	}

	select {
	case numMessages := <-updates:
		if numMessages != selected.NumMessages {
			t.Fatalf("unexpected IDLE EXISTS update: got %d, want %d", numMessages, selected.NumMessages)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for IDLE update")
	}

	if err = idleCmd.Close(); err != nil {
		t.Fatal(err)
	}
	if err = idleCmd.Wait(); err != nil {
		t.Fatal(err)
	}
	waitIdleConnectionsDeleted(t, user.ID)
}

func readTaggedLine(reader *bufio.Reader, tag string) (string, error) {
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return line, err
		}
		if strings.HasPrefix(line, tag+" ") {
			return line, nil
		}
	}
}

func waitIdleConnectionsDeleted(t *testing.T, userId int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, ok := userConnects.Load(userId); !ok {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("idle connections were not cleaned up")
}
func TestUnselect(t *testing.T) {

}

func TestLogout(t *testing.T) {
	err := clientLogin.Logout().Wait()
	if err != nil {
		t.Error(err)
	}
}
