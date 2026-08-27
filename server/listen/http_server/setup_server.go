package http_server

import (
	"errors"
	"flag"
	"fmt"
	"git.pcux.cn/xuans/Smail/config"
	"git.pcux.cn/xuans/Smail/controllers"
	"git.pcux.cn/xuans/Smail/utils/ip"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cast"
	"io/fs"
	"net"
	"net/http"
	"os"
	"strings"
	"time"
)

// 项目初始化引导用的服务，初始化引导结束后即退出
var setupServer *http.Server

func SetupStart() error {
	mux := http.NewServeMux()
	setupSecurity, err := newSetupSecurity()
	if err != nil {
		return fmt.Errorf("initialize setup security: %w", err)
	}
	fe, err := fs.Sub(local, "dist")
	if err != nil {
		return fmt.Errorf("load embedded setup frontend: %w", err)
	}
	mux.Handle("/", setupSecurity.bootstrap(http.FileServer(http.FS(fe))))
	mux.HandleFunc("/api/setup", setupSecurity.authorize(setupOnlyPost(contextIterceptor(controllers.Setup))))
	// 挑战请求类似这样 /.well-known/acme-challenge/QPyMAyaWw9s5JvV1oruyqWHG7OqkHMJEHPoUz2046KM
	mux.HandleFunc("/.well-known/", controllers.AcmeChallenge)

	HttpPort := 80
	flag.IntVar(&HttpPort, "p", 80, "初始化阶段Http服务端口")
	flag.Parse()

	if HttpPort == 80 {
		HttpPort = cast.ToInt(os.Getenv("setup_port"))
	}

	if HttpPort <= 0 || HttpPort > 65535 {
		HttpPort = 80
	}

	config.SetRuntimeSetupPort(HttpPort)
	log.Infof("HttpServer Start On Port :%d", HttpPort)
	if HttpPort == 80 {
		log.Infof("Please click http://%s to continue.\n", ip.GetIp())
	} else {
		log.Infof("Please click http://%s:%d to continue.", ip.GetIp(), HttpPort)
	}

	setupHost := strings.TrimSpace(os.Getenv("SMAIL_SETUP_HOST"))
	if setupHost == "" {
		setupHost = "127.0.0.1"
	}
	if setupHost != "127.0.0.1" && setupHost != "localhost" {
		if net.ParseIP(setupHost) == nil || strings.TrimSpace(os.Getenv("SMAIL_SETUP_ALLOWED_CIDRS")) == "" || strings.TrimSpace(os.Getenv("SMAIL_SETUP_TOKEN")) == "" {
			setupHost = "127.0.0.1"
		}
	}
	setupServer = &http.Server{
		Addr:              fmt.Sprintf("%s:%d", setupHost, HttpPort),
		Handler:           mux,
		ReadTimeout:       time.Second * 60,
		ReadHeaderTimeout: time.Second * 10,
		WriteTimeout:      time.Second * 60,
		IdleTimeout:       time.Second * 30,
		MaxHeaderBytes:    16 << 10,
	}
	err = setupServer.ListenAndServe()
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("serve setup HTTP: %w", err)
	}
	return nil
}

func setupOnlyPost(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		next.ServeHTTP(w, r)
	}
}

func SetupStop() {
	if setupServer == nil {
		return
	}
	err := setupServer.Close()
	setupServer = nil
	log.Infof("Setup End!")
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.WithError(err).Warn("close setup HTTP server failed")
	}
}
