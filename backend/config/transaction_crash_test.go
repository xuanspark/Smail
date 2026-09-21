package config

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

const transactionCrashRoot = "SMAIL_TEST_TRANSACTION_CRASH_ROOT"
const transactionCrashPoint = "SMAIL_TEST_TRANSACTION_CRASH_POINT"

func crashOptions() ConfigLoaderOptions {
	return ConfigLoaderOptions{Validation: RuntimeValidationOptions{HasSuperAdmin: true, Production: true}}
}

func crashCandidate(current RuntimeConfig) RuntimeConfig {
	candidate := cloneRuntimeConfig(current)
	setCandidateGeneration(&candidate, current.Generation+1)
	candidate.Email.Policy.MaxMessageBytes = 9 << 20
	if candidate.Root.Database.Type == DatabaseTypeSQLite {
		candidate.Root.Database.LocalRef = "next.db"
	} else {
		candidate.Root.Database.Name = "smail_next"
	}
	return candidate
}

func crashTransactionDirectory(t *testing.T, root string) string {
	t.Helper()
	base := filepath.Join(root, "data", "transaction")
	entries, err := os.ReadDir(base)
	if err != nil || len(entries) != 1 {
		t.Fatalf("事务目录数量异常：%d，%v", len(entries), err)
	}
	return filepath.Join(base, entries[0].Name())
}

// TestTransactionCrashChild 只在测试二进制中运行，不向正式程序添加故障注入入口。
func TestTransactionCrashChild(t *testing.T) {
	root, point := os.Getenv(transactionCrashRoot), os.Getenv(transactionCrashPoint)
	if root == "" {
		return
	}
	current, err := NewConfigLoader(root, crashOptions()).Load()
	if err != nil {
		t.Fatal(err)
	}
	phase := TransactionPhase(point)
	if point == "partial_sections" {
		phase = TransactionPrepared
	}
	if point == "root_before_manifest" {
		phase = TransactionSectionsCommitted
	}
	store := NewTransactionStore(root, crashOptions().Validation).WithHooks(TransactionHooks{
		BeforePhase: func(reached TransactionPhase) error {
			if reached != phase {
				return nil
			}
			// 复用正式原子替换函数构造阶段内部的落盘状态；不声称命中了实际指令间隙。
			if point == "partial_sections" || point == "root_before_manifest" {
				name := "config.json"
				if point == "partial_sections" {
					name, _ = FixedSectionFile(requiredPartitionSections[0])
				}
				source := filepath.Join(crashTransactionDirectory(t, root), "candidate", name)
				if err := replaceLiveConfigFile(filepath.Join(root, "config"), source, name); err != nil {
					return err
				}
			}
			if err := os.Mkdir(filepath.Join(root, "crash-ready"), 0700); err != nil {
				return err
			}
			// 必须等待父进程强制终止，不能返回错误而执行提交函数的 defer。
			for {
				time.Sleep(time.Hour)
			}
		},
	})
	if err := store.Commit(current, crashCandidate(current)); err != nil {
		t.Fatal(err)
	}
	t.Fatal("子进程没有停在要求的提交阶段")
}

func killTransactionChild(t *testing.T, root, point string) {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, executable, "-test.run=^TestTransactionCrashChild$", "-test.timeout=45s")
	command.Env = append(os.Environ(), transactionCrashRoot+"="+root, transactionCrashPoint+"="+point)
	var output bytes.Buffer
	command.Stdout, command.Stderr = &output, &output
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	wait := make(chan error, 1)
	go func() { wait <- command.Wait() }()
	finished := false
	defer func() {
		if !finished {
			_ = command.Process.Kill()
			<-wait
		}
	}()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case err := <-wait:
			finished = true
			t.Fatalf("到达故障点前子进程退出：%v\n%s", err, output.String())
		case <-ctx.Done():
			t.Fatal("等待事务故障点超时")
		case <-ticker.C:
			ready, err := os.Stat(filepath.Join(root, "crash-ready"))
			if os.IsNotExist(err) {
				continue
			}
			if err != nil || !ready.IsDir() {
				t.Fatalf("故障点确认失败：%v", err)
			}
			if err := command.Process.Kill(); err != nil {
				t.Fatal(err)
			}
			err = <-wait
			finished = true
			var exitError *exec.ExitError
			if !errors.As(err, &exitError) || command.ProcessState.Success() {
				t.Fatalf("子进程不是强制终止：%v", err)
			}
			t.Logf("已确认阶段 %s，子进程 %d，退出状态 %s", point, command.Process.Pid, command.ProcessState)
			return
		}
	}
}

func crashFixture(t *testing.T, kind DatabaseType) (string, RuntimeConfig) {
	t.Helper()
	root := t.TempDir()
	if err := InitializeDeployment(root); err != nil {
		t.Fatal(err)
	}
	current := validRuntimeConfig()
	if kind != DatabaseTypeSQLite {
		current = validMySQLRuntimeConfig(t, root)
		current.Root.Database.Type = kind
		// 本专项只验证配置落盘恢复，不访问共享数据库或创建替代 SQLite。
		current.Root.Database.Port = 1
	}
	writeRuntimeFixture(t, root, current)
	loaded, err := NewConfigLoader(root, crashOptions()).Load()
	if err != nil {
		t.Fatal(err)
	}
	current = loaded
	return root, current
}

func readCrashConfigFiles(t *testing.T, directory string) map[string][]byte {
	t.Helper()
	files := make(map[string][]byte)
	for _, name := range transactionFileNames() {
		data, err := os.ReadFile(filepath.Join(directory, name))
		if err != nil {
			t.Fatal(err)
		}
		files[name] = data
	}
	return files
}

func assertCrashConfigFiles(t *testing.T, root string, expected map[string][]byte) {
	t.Helper()
	for name, want := range expected {
		got, err := os.ReadFile(filepath.Join(root, "config", name))
		if err != nil || !bytes.Equal(got, want) {
			t.Fatalf("恢复后的配置文件不匹配：%s，%v", name, err)
		}
	}
}

func TestTransactionProcessCrashRecovery(t *testing.T) {
	cases := []struct {
		point string
		phase TransactionPhase
		new   bool
	}{
		{"prepared", TransactionPrepared, false},
		{"partial_sections", TransactionPrepared, false},
		{"sections_committed", TransactionSectionsCommitted, false},
		{"root_before_manifest", TransactionSectionsCommitted, true},
		{"root_committed", TransactionRootCommitted, true},
		{"verified", TransactionVerified, true},
	}
	for _, kind := range []DatabaseType{DatabaseTypeSQLite, DatabaseTypeMySQL, DatabaseTypePostgreSQL} {
		t.Run(string(kind), func(t *testing.T) {
			for _, test := range cases {
				t.Run(test.point, func(t *testing.T) {
					root, current := crashFixture(t, kind)
					killTransactionChild(t, root, test.point)
					manifest, err := readTransactionManifest(crashTransactionDirectory(t, root))
					if err != nil || manifest.Phase != test.phase {
						t.Fatalf("强制退出后事务清单状态异常：%v", err)
					}
					before, loadErr := NewConfigLoader(root, crashOptions()).Load()
					partial := test.point == "partial_sections" || test.point == "sections_committed"
					if partial && loadErr == nil {
						t.Fatal("尚未恢复的不完整配置被错误接受")
					}
					want := current
					if test.new {
						want = crashCandidate(current)
					}
					part := "original"
					if test.new {
						part = "candidate"
					}
					files := readCrashConfigFiles(t, filepath.Join(crashTransactionDirectory(t, root), part))
					if !partial && (loadErr != nil || before.Generation != want.Generation) {
						t.Fatalf("恢复前完整配置代次异常：%v", loadErr)
					}
					// 两次走正式启动恢复路径，同时验证锁释放和恢复幂等。
					for attempt := 0; attempt < 2; attempt++ {
						manager, err := NewConfigManager(root, crashOptions())
						if err != nil {
							t.Fatalf("第 %d 次启动恢复失败：%v", attempt+1, err)
						}
						got := manager.Snapshot()
						if got.Generation != want.Generation || !reflect.DeepEqual(got.Root.Database, want.Root.Database) {
							t.Fatal("启动后选中的数据库或配置代次不匹配")
						}
						assertCrashConfigFiles(t, root, files)
					}
					entries, err := os.ReadDir(filepath.Join(root, "data", "transaction"))
					if err != nil || len(entries) != 0 {
						t.Fatalf("恢复后仍有活动事务：%v", err)
					}
					entries, err = os.ReadDir(filepath.Join(root, "data"))
					if err != nil {
						t.Fatal(err)
					}
					for _, entry := range entries {
						if entry.Name() != ".config-state.lock" && entry.Name() != "transaction" {
							t.Fatalf("纯配置恢复意外创建数据文件：%s", entry.Name())
						}
					}
				})
			}
		})
	}
}

func TestTransactionProcessCrashRejectsTamperedRecovery(t *testing.T) {
	for _, part := range []string{"original", "candidate"} {
		t.Run(part, func(t *testing.T) {
			root, _ := crashFixture(t, DatabaseTypeSQLite)
			files := readCrashConfigFiles(t, filepath.Join(root, "config"))
			killTransactionChild(t, root, "prepared")
			directory := crashTransactionDirectory(t, root)
			path := filepath.Join(directory, part, "config.json")
			if err := os.WriteFile(path, []byte("{}"), 0600); err != nil {
				t.Fatal(err)
			}
			for attempt := 0; attempt < 2; attempt++ {
				if _, err := NewConfigManager(root, crashOptions()); !errors.Is(err, ErrConfigDigestMismatch) {
					t.Fatalf("损坏事务没有失败关闭：%v", err)
				}
				assertCrashConfigFiles(t, root, files)
				if _, err := os.Stat(path); err != nil {
					t.Fatal("失败关闭不应删除损坏证据")
				}
			}
		})
	}
}

// TestTransactionUnpublishedDirectoryFailsClosed 复现首份清单发布前的磁盘状态。
// 无法证明来源的残留必须保留并阻止启动，不能把安全拒绝报告成自动恢复成功。
func TestTransactionUnpublishedDirectoryFailsClosed(t *testing.T) {
	root, _ := crashFixture(t, DatabaseTypeSQLite)
	files := readCrashConfigFiles(t, filepath.Join(root, "config"))
	directory := filepath.Join(root, "data", "transaction", "0123456789abcdef0123456789abcdef")
	for _, part := range []string{"original", "candidate"} {
		if err := os.MkdirAll(filepath.Join(directory, part), 0700); err != nil {
			t.Fatal(err)
		}
	}
	for attempt := 0; attempt < 2; attempt++ {
		if _, err := NewConfigManager(root, crashOptions()); !errors.Is(err, ErrTransactionInvalid) {
			t.Fatalf("无清单残留没有阻止启动：%v", err)
		}
		assertCrashConfigFiles(t, root, files)
		if _, err := os.Stat(directory); err != nil {
			t.Fatal("无清单残留被错误删除")
		}
	}
}
