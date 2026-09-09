package app

import (
	"fmt"
	"os"

	"git.pcux.cn/xuans/Smail/config"
	"git.pcux.cn/xuans/Smail/internal/buildinfo"
	"git.pcux.cn/xuans/Smail/listen/cron_server"
	"git.pcux.cn/xuans/Smail/res_init"
	"git.pcux.cn/xuans/Smail/services/runtimelog"
	"git.pcux.cn/xuans/Smail/utils/runtimepath"
	log "github.com/sirupsen/logrus"
)

// Run 启动 Smail 主服务。
func Run() (err error) {
	if err := config.InitializeDeployment(config.ROOT_PATH); err != nil {
		return err
	}
	if err := runtimepath.CheckReady(config.ROOT_PATH); err != nil {
		return err
	}
	if err := config.MigrateLegacySSLStorage(config.ROOT_PATH); err != nil {
		return fmt.Errorf("迁移证书存储失败，原文件已保留: %w", err)
	}
	if err := config.Init(); err != nil {
		return err
	}
	runtimeLogs, err := runtimelog.Start(config.ROOT_PATH)
	if err != nil {
		return fmt.Errorf("启动运行日志失败: %w", err)
	}
	defer finishRuntimeLogs(runtimeLogs, &err)
	config.MaintainManagedSSLHistory()

	build := buildinfo.Current()
	if build.Version == "dev" {
		build.Version = "TestVersion"
	}

	log.WithFields(log.Fields{"action": "启动主服务", "object": "Smail", "result": "started"}).Infof("服务启动，版本：%s", build.Version)

	// 启动定时任务。
	go cron_server.Start()

	// 启动核心服务。
	if err := res_init.Init(build.Version); err != nil {
		return err
	}

	log.WithFields(log.Fields{"action": "停止主服务", "object": "Smail", "result": "success"}).Info("主服务已停止")
	return nil
}

// finishRuntimeLogs 在关闭日志文件前记录返回错误或异常，避免致命错误只留在控制台。
func finishRuntimeLogs(writer *runtimelog.Writer, runErr *error) {
	if recovered := recover(); recovered != nil {
		*runErr = fmt.Errorf("服务启动异常: %v", recovered)
	}
	if *runErr != nil {
		log.WithError(*runErr).WithFields(log.Fields{"action": "运行主服务", "object": "Smail", "result": "failure"}).Error("服务因致命错误停止")
	}
	if err := writer.Close(); err != nil {
		log.WithError(err).Error("关闭运行日志失败")
	}
}

// RunAndExit 执行主服务并在发生致命错误时退出。
func RunAndExit() {
	if err := Run(); err != nil {
		log.WithError(err).Error("Server stopped because of a fatal error")
		os.Exit(1)
	}
}

// RecoverPanic 将启动过程中的 panic 转换为可记录的错误。
func RecoverPanic(run func() error) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("server initialization panic: %v", recovered)
		}
	}()
	return run()
}
