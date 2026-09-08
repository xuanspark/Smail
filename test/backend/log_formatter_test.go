package config

import (
	"context"
	"encoding/json"
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

func TestRuntimeEventMetadataNeverInventsSuccessfulCompletion(t *testing.T) {
	for _, item := range []struct {
		message, result string
		data            log.Fields
	}{
		{"包含成功文字但未提供完成信号", "recorded", nil},
		{"开始启动", "started", log.Fields{"action": "启动服务", "result": "started"}},
		{"连接失败", "failure", log.Fields{"action": "连接数据库", "result": "failure"}},
	} {
		entry := &log.Entry{Time: time.Now(), Level: log.WarnLevel, Context: context.Background(), Message: item.message, Data: item.data}
		data, err := (&logFormatter{}).Format(entry)
		if err != nil {
			t.Fatal(err)
		}
		parts := strings.Split(string(data), " @event ")
		var fields map[string]any
		if len(parts) != 2 || json.Unmarshal([]byte(parts[1]), &fields) != nil {
			t.Fatalf("无效元数据：%s", data)
		}
		if fields["result"] != item.result || fields["action"] == "" || fields["logSchema"] != float64(1) {
			t.Fatalf("结果错误：%+v", fields)
		}
		if _, changed := item.data["logSchema"]; changed {
			t.Fatal("格式器修改了调用者字段")
		}
	}
}
