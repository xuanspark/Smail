package app

import (
	"fmt"
	"os"

	"git.pcux.cn/xuans/Smail/config"
	"git.pcux.cn/xuans/Smail/internal/buildinfo"
	"git.pcux.cn/xuans/Smail/listen/cron_server"
	"git.pcux.cn/xuans/Smail/res_init"
	log "github.com/sirupsen/logrus"
)

// Run 启动 Smail 主服务。
func Run() error {
	if err := config.Init(); err != nil {
		return err
	}

	build := buildinfo.Current()
	if build.Version == "dev" {
		build.Version = "TestVersion"
	}

	log.Infoln("*******************************************************************")
	log.Infof("***\tServer Start Success \n")
	log.Infof("***\tServer Version: %s \n", build.Version)
	log.Infof("***\tGit Commit Hash: %s ", build.GitHash)
	log.Infof("***\tBuild Date: %s ", build.BuildTime)
	log.Infof("***\tBuild GoLang Version: %s ", build.GoVersion)
	log.Infoln("*******************************************************************")

	// 启动定时任务。
	go cron_server.Start()

	// 启动核心服务。
	if err := res_init.Init(build.Version); err != nil {
		return err
	}

	log.Warnf("Server Stoped \n")
	return nil
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
