package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"git.pcux.cn/xuans/Smail/config"
)

// main 提供部署端显式配置迁移，不在网页或普通启动中自动改写配置。
func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	flags := flag.NewFlagSet("smail-config", flag.ContinueOnError)
	root := flags.String("root", "", "Smail 服务根目录，包含 config 子目录")
	apply := flags.Bool("apply", false, "确认已停止后端，备份并执行迁移；默认只检查")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *root == "" || flags.NArg() != 0 {
		return fmt.Errorf("请指定 --root 服务根目录；迁移前必须停止后端")
	}
	absolute, err := filepath.Abs(*root)
	if err != nil {
		return fmt.Errorf("服务根目录无效")
	}
	options := config.RuntimeValidationOptions{HasSuperAdmin: true, Production: true}
	plan, err := config.PlanLegacyMigration(absolute, options)
	if err != nil {
		// 不输出配置原文、未知字段或底层错误，避免凭据进入终端记录。
		return fmt.Errorf("迁移检查未通过：未知字段 %d 项，需人工处理字段 %d 项；未执行迁移", len(plan.UnknownFields), len(plan.ManualFields))
	}
	if !*apply {
		fmt.Println("旧版字段检查通过；尚未写入配置。执行迁移前请停止后端，使用 --apply 继续完整校验和备份迁移。")
		return nil
	}
	result, err := config.MigrateLegacyConfig(absolute, options)
	if err != nil {
		return fmt.Errorf("配置迁移未完成，请保留原配置和备份，由部署管理员检查；不要手工删除分区文件")
	}
	fmt.Printf("配置迁移完成，代次：%d；原配置备份：%s。请使用最新版后端启动。\n", result.Generation, result.BackupPath)
	return nil
}
