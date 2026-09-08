package config

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func requireIndentedJSON(t *testing.T, data []byte) {
	t.Helper()
	var expected bytes.Buffer
	if err := json.Indent(&expected, data, "", "  "); err != nil {
		t.Fatal("配置不是有效 JSON")
	}
	if !bytes.Equal(data, expected.Bytes()) || !bytes.Contains(data, []byte("\n  \"")) {
		t.Fatal("配置未按两空格缩进分行保存")
	}
}

func TestRuntimeJSONFormattingPreservesValuesAndDigests(t *testing.T) {
	runtime := validRuntimeConfig()
	runtime.Plugins.Plugins["telegram_push"] = PluginInstance{
		Settings: json.RawMessage(`{"label":"中文配置","nested":{"enabled":true},"items":["邮件",9007199254740993]}`),
	}
	files, err := marshalRuntimeFiles(runtime)
	if err != nil {
		t.Fatal(err)
	}
	for _, data := range files {
		requireIndentedJSON(t, data)
	}
	var root RootConfig
	if err := json.Unmarshal(files["config.json"], &root); err != nil {
		t.Fatal(err)
	}
	for section, reference := range root.Sections {
		name, _ := FixedSectionFile(section)
		if reference.SHA256 != SectionDigest(files[name]) {
			t.Fatal("根配置未使用格式化后的摘要")
		}
	}
	var plugins PluginsSectionConfig
	if err := json.Unmarshal(files["plugins.json"], &plugins); err != nil {
		t.Fatal(err)
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, plugins.Plugins["telegram_push"].Settings); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(compact.Bytes(), runtime.Plugins.Plugins["telegram_push"].Settings) {
		t.Fatal("格式化改变了中文、嵌套字段或大整数")
	}
}

func TestRuntimeJSONFormattingLoadsCompactHistoryAndPersistsIndented(t *testing.T) {
	for _, deployment := range []bool{false, true} {
		name := "legacy"
		if deployment {
			name = "deployment"
		}
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			if deployment {
				if err := InitializeDeployment(root); err != nil {
					t.Fatal(err)
				}
			}
			current := validRuntimeConfig()
			files, err := marshalRuntimeFiles(current)
			if err != nil {
				t.Fatal(err)
			}
			var oldRoot RootConfig
			if err := json.Unmarshal(files["config.json"], &oldRoot); err != nil {
				t.Fatal(err)
			}
			// 模拟真实的旧版单行文件，仍使用原字节的摘要。
			for section, reference := range oldRoot.Sections {
				var compact bytes.Buffer
				if err := json.Compact(&compact, files[reference.File]); err != nil {
					t.Fatal(err)
				}
				files[reference.File] = compact.Bytes()
				reference.SHA256 = SectionDigest(compact.Bytes())
				oldRoot.Sections[section] = reference
			}
			files["config.json"], err = json.Marshal(oldRoot)
			if err != nil {
				t.Fatal(err)
			}
			directory, err := secureManagerConfigDirectory(root)
			if err != nil {
				t.Fatal(err)
			}
			for filename, data := range files {
				if err := os.WriteFile(filepath.Join(directory, filename), data, 0600); err != nil {
					t.Fatal(err)
				}
			}
			validation := RuntimeValidationOptions{HasSuperAdmin: true}
			loader := NewConfigLoader(root, ConfigLoaderOptions{Validation: validation})
			loaded, err := loader.Load()
			if err != nil || loaded.Generation != current.Generation {
				t.Fatalf("旧版单行配置不能读取：%v", err)
			}
			store := NewTransactionStore(root, validation)
			if _, err := store.CreateBackup(loaded); err != nil {
				t.Fatal(err)
			}
			if _, err := store.LoadBackup(loaded.Generation); err != nil {
				t.Fatal(err)
			}
			candidate := cloneRuntimeConfig(loaded)
			setCandidateGeneration(&candidate, loaded.Generation+1)
			if err := store.Commit(loaded, candidate); err != nil {
				t.Fatal(err)
			}
			if _, err := loader.Load(); err != nil {
				t.Fatal(err)
			}
			for filename := range files {
				data, err := os.ReadFile(filepath.Join(directory, filename))
				if err != nil {
					t.Fatal(err)
				}
				requireIndentedJSON(t, data)
			}
			entries, err := os.ReadDir(directory)
			if err != nil || len(entries) != 5 {
				t.Fatal("格式化不应生成额外配置文件")
			}
		})
	}
}

func TestAuditRemainsJSONLAfterConfigFormatting(t *testing.T) {
	writer, err := NewFileAuditWriter(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 2; index++ {
		if err := writer.WriteConfigAudit(nil, ConfigAuditEvent{
			Operation: ConfigOperationUpdate, Section: SectionRoot,
			ActorID: "1", ActorRole: "super_admin", FromGeneration: 1,
			ToGeneration: 2, Result: "success", CreatedAt: time.Now().UTC(),
		}); err != nil {
			t.Fatal(err)
		}
	}
	data, err := os.ReadFile(writer.path)
	if err != nil {
		t.Fatal(err)
	}
	lines := bytes.Split(bytes.TrimSpace(data), []byte("\n"))
	if len(lines) != 2 || !json.Valid(lines[0]) || !json.Valid(lines[1]) {
		t.Fatal("本次排版不能改变逐行审计格式")
	}
}
