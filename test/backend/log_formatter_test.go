package config

import (
	"errors"
	"runtime"
	"strings"
	"testing"
	"time"

	log "github.com/sirupsen/logrus"
)

func TestLogFormatterPreservesStructuredEvidence(t *testing.T) {
	formatter := &logFormatter{}
	for _, message := range []string{"", "连接失败"} {
		entry := &log.Entry{
			Time: time.Now(), Level: log.InfoLevel, Message: message,
			Caller: &runtime.Frame{File: "test.go", Line: 12},
			Data:   log.Fields{"error": errors.New("测试错误\n下一行"), "code": 0, "unsupported": make(chan int)},
		}
		data, err := formatter.Format(entry)
		if err != nil {
			t.Fatal(err)
		}
		text := string(data)
		for _, expected := range []string{`"error":"测试错误\n下一行"`, `"code":0`, "无法序列化的字段", message} {
			if !strings.Contains(text, expected) {
				t.Fatalf("字段丢失：%s", text)
			}
		}
		if strings.Count(text, "\n") != 1 {
			t.Fatal("结构化错误中的换行不应生成伪记录")
		}
	}
}
