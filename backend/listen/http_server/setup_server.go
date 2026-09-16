package http_server

import (
	"errors"
	"flag"
	"fmt"
	"git.pcux.cn/xuans/Smail/config"
	"git.pcux.cn/xuans/Smail/controllers"
	"git.pcux.cn/xuans/Smail/dto/response"
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
	registerSetupAPIRoutes(mux, setupSecurity, setupOnlyPost(contextIterceptor(controllers.Setup)))
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

// registerSetupAPIRoutes 为旧初始化入口和 V1 初始化入口安装完全相同的安全链路。
// 两个入口只承担兼容关系，不允许因为路径版本不同而出现授权差异。
func registerSetupAPIRoutes(mux *http.ServeMux, security *setupSecurity, next http.HandlerFunc) {
	if mux == nil || security == nil || next == nil {
		return
	}
	authorized := security.authorize(next)
	mux.HandleFunc("/api/setup", authorized)
	mux.HandleFunc("/api/v1/setup", authorized)
	// 安装阶段尚无业务会话，通过受保护的状态接口引导前端进入安装向导。
	status := security.authorize(setupOnlyPost(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		response.NewErrorResponse(response.NeedSetup, "请先完成初始化", "").FPrint(w)
	}))
	mux.HandleFunc("/api/user/info", status)
	mux.HandleFunc("/api/v1/user/info", status)
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
