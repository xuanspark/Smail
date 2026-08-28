package main

import (
	"fmt"
	"os"

	"git.pcux.cn/xuans/Smail/config"
	"git.pcux.cn/xuans/Smail/internal/buildinfo"
	"git.pcux.cn/xuans/Smail/listen/cron_server"
	"git.pcux.cn/xuans/Smail/res_init"
	log "github.com/sirupsen/logrus"
)

func main() {
	if err := run(); err != nil {
		log.WithError(err).Error("Server stopped because of a fatal error")
		os.Exit(1)
	}
}

func run() (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("server initialization panic: %v", recovered)
		}
	}()

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

	// 定时任务启动
	go cron_server.Start()

	// 核心服务启动
	if err := res_init.Init(build.Version); err != nil {
		return err
	}

	log.Warnf("Server Stoped \n")
	return nil
}
