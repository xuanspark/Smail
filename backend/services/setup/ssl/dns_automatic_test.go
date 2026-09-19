package ssl

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"git.pcux.cn/xuans/Smail/config"
)

func TestAutomaticDNSCleansOnlyOwnedUnchangedRecords(t *testing.T) {
	t.Setenv("LEGO_DISABLE_CNAME_SUPPORT", "true")
	var mu sync.Mutex
	records := map[string]aliDNSRecord{"existing": {ID: "existing", Name: "_acme-challenge", Value: "unrelated-record"}}
	adds, deletes := 0, 0
	failDelete, tamper := false, false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if r.Method != http.MethodPost || r.ParseForm() != nil || r.Form.Get("AccessKeyId") != "test-access" || r.Form.Get("Signature") == "" {
			t.Error("invalid DNS API request")
			w.WriteHeader(400)
			return
		}
		id := r.Form.Get("RecordId")
		switch r.Form.Get("Action") {
		case "AddDomainRecord":
			adds++
			id = fmt.Sprint(adds)
			records[id] = aliDNSRecord{ID: id, Name: r.Form.Get("RR"), Value: r.Form.Get("Value")}
			_ = json.NewEncoder(w).Encode(map[string]string{"RecordId": id})
		case "DescribeDomainRecordInfo":
			record := records[id]
			if tamper { record.Value = "changed-by-user" }
			_ = json.NewEncoder(w).Encode(map[string]string{"RecordId": id, "DomainName": "example.com", "RR": record.Name, "Type": "TXT", "Value": record.Value})
		case "DeleteDomainRecord":
			if failDelete { w.WriteHeader(503); _, _ = w.Write([]byte(`{"Code":"Busy"}`)); return }
			deletes++
			delete(records, id)
			_ = json.NewEncoder(w).Encode(map[string]string{"RecordId": id})
		default:
			t.Error("unexpected DNS action")
			w.WriteHeader(400)
		}
	}))
	defer server.Close()
	provider := &aliDNSChallenge{zone: "example.com", accessKey: "test-access", secretKey: "test-secret", endpoint: server.URL, client: server.Client(), records: map[string]aliDNSRecord{}}
	var workers sync.WaitGroup
	for _, domain := range []string{"example.com", "*.example.com"} {
		for i := 0; i < 3; i++ {
			workers.Add(1)
			go func(domain string) {
				defer workers.Done()
				if err := provider.Present(domain, "token", "key-"+domain); err != nil { t.Error(err) }
			}(domain)
		}
	}
	workers.Wait()
	if adds != 2 || len(records) != 3 { t.Fatalf("same-name TXT records lost or duplicated: adds=%d records=%d", adds, len(records)) }
	if err := provider.CleanUp("other.example.com", "token", "unknown"); err != nil || deletes != 0 { t.Fatal("unknown challenge must not touch DNS") }
	mu.Lock(); tamper = true; mu.Unlock()
	if err := provider.CleanUp("example.com", "token", "key-example.com"); err == nil || deletes != 0 { t.Fatal("modified record must be retained") }
	mu.Lock(); tamper = false; failDelete = true; mu.Unlock()
	if err := provider.CleanUp("example.com", "token", "key-example.com"); err == nil || len(provider.records) != 2 { t.Fatal("failed deletion must retain ownership for retry") }
	mu.Lock(); failDelete = false; mu.Unlock()
	if err := provider.CleanUp("example.com", "token", "key-example.com"); err != nil { t.Fatal(err) }
	if len(records) != 2 { t.Fatal("cleaning root challenge removed wildcard or unrelated TXT") }
	if err := provider.CleanUp("*.example.com", "token", "key-*.example.com"); err != nil { t.Fatal(err) }
	if len(records) != 1 || records["existing"].Value != "unrelated-record" { t.Fatal("existing TXT must survive all challenge cleanup") }
	if err := provider.CleanUp("*.example.com", "token", "key-*.example.com"); err != nil || deletes != 2 { t.Fatal("cleanup must be idempotent") }
}

func TestAutomaticDNSRejectsOutsideZoneAndRedactsErrors(t *testing.T) {
	t.Setenv("LEGO_DISABLE_CNAME_SUPPORT", "true")
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(403)
		_, _ = w.Write([]byte(`{"Code":"Denied","Message":"test-access test-secret signature"}`))
	}))
	defer server.Close()
	provider := &aliDNSChallenge{zone: "example.com", accessKey: "test-access", secretKey: "test-secret", endpoint: server.URL, client: server.Client(), records: map[string]aliDNSRecord{}}
	if err := provider.Present("unrelated.com", "token", "key"); err == nil || calls != 0 { t.Fatal("out-of-zone write attempted") }
	err := provider.Present("example.com", "token", "key")
	if err == nil || calls != 1 { t.Fatal("provider refusal not reported") }
	for _, secret := range []string{"test-access", "test-secret", "signature"} {
		if strings.Contains(err.Error(), secret) { t.Fatal("error leaked provider response") }
	}
	for _, management := range []string{config.SSLManagementCustom, config.SSLManagementWebServer} {
		if _, err := automaticDNSProvider(&config.Config{SSL: config.SSLConfig{Management: management, Challenge: config.SSLChallengeDNS}}); err == nil { t.Fatal("external certificate mode allowed automatic DNS") }
	}
}
