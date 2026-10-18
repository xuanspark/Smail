package translate

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestTranslateEmptyText(t *testing.T) {
	res, err := TranslateText(context.Background(), "", "zh-CN")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.TranslatedText != "" {
		t.Fatalf("expected empty translated text, got %q", res.TranslatedText)
	}
}

func TestTranslateMockServer(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST method, got %s", r.Method)
		}
		if err := r.ParseForm(); err != nil {
			t.Errorf("parse form failed: %v", err)
		}
		q := r.Form.Get("q")
		if q != "Hello world" {
			t.Errorf("expected q='Hello world', got %q", q)
		}

		w.Header().Set("Content-Type", "application/json")
		// 模拟 Google GTX 翻译返回结构
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`[[["你好世界","Hello world",null,null,10]],null,"en"]`))
	}))
	defer ts.Close()

	oldURL := apiBaseURL
	oldClient := defaultClient
	defer func() {
		SetApiBaseURL(oldURL)
		SetDefaultClient(oldClient)
	}()

	SetApiBaseURL(ts.URL)
	SetDefaultClient(ts.Client())

	res, err := TranslateText(context.Background(), "Hello world", "zh-CN")
	if err != nil {
		t.Fatalf("TranslateText failed: %v", err)
	}

	if res.TranslatedText != "你好世界" {
		t.Fatalf("expected '你好世界', got %q", res.TranslatedText)
	}
	if res.SourceLang != "en" {
		t.Fatalf("expected source lang 'en', got %q", res.SourceLang)
	}
}
