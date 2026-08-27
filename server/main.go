package main

import (
	"fmt"
	"os"

	"git.pcux.cn/xuans/Smail/config"
	"git.pcux.cn/xuans/Smail/listen/cron_server"
	"git.pcux.cn/xuans/Smail/res_init"
	log "github.com/sirupsen/logrus"
)

var (
	gitHash   string
	buildTime string
	goVersion string
	version   string
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

	if version == "" {
		version = "TestVersion"
	}

	log.Infoln("*******************************************************************")
	log.Infof("***\tServer Start Success \n")
	log.Infof("***\tServer Version: %s \n", version)
	log.Infof("***\tGit Commit Hash: %s ", gitHash)
	log.Infof("***\tBuild Date: %s ", buildTime)
	log.Infof("***\tBuild GoLang Version: %s ", goVersion)
	log.Infoln("*******************************************************************")

	// 定时任务启动
	go cron_server.Start()

	// 核心服务启动
	if err := res_init.Init(version); err != nil {
		return err
	}

	log.Warnf("Server Stoped \n")
	return nil
}
