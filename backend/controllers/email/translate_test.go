package email

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"git.pcux.cn/xuans/Smail/services/translate"
	"git.pcux.cn/xuans/Smail/utils/context"
)

func TestTranslateEmailDirectText(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[[["你好","Hello",null,null,10]],null,"en"]`))
	}))
	defer ts.Close()

	translate.SetApiBaseURL(ts.URL)
	translate.SetDefaultClient(ts.Client())

	reqBody := emailTranslateRequest{
		Subject:    "Hello",
		Text:       "Hello",
		TargetLang: "zh-CN",
	}
	bodyBytes, _ := json.Marshal(reqBody)

	r := httptest.NewRequest(http.MethodPost, "/api/v1/email/translate", bytes.NewReader(bodyBytes))
	w := httptest.NewRecorder()

	ctx := &context.Context{UserID: 1}
	TranslateEmail(ctx, w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d, body: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Code int                    `json:"code"`
		Data emailTranslateResponse `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response failed: %v", err)
	}

	if resp.Data.SubjectTranslated != "你好" {
		t.Errorf("expected SubjectTranslated='你好', got %q", resp.Data.SubjectTranslated)
	}
	if resp.Data.ContentTranslated != "你好" {
		t.Errorf("expected ContentTranslated='你好', got %q", resp.Data.ContentTranslated)
	}
}
