package http_server

import (
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"git.pcux.cn/xuans/Smail/config"
	"git.pcux.cn/xuans/Smail/controllers"
	"git.pcux.cn/xuans/Smail/db"
	"git.pcux.cn/xuans/Smail/models"
	"git.pcux.cn/xuans/Smail/services/access"
	"git.pcux.cn/xuans/Smail/services/auth"
	"git.pcux.cn/xuans/Smail/services/credentials"
	"git.pcux.cn/xuans/Smail/services/externalemail"
	"git.pcux.cn/xuans/Smail/session"
)

// TestSecuritySetupBrowserFixture 仅显式启用时运行，隔离数据库并复用正式认证路由。
// 此服务不启动邮件协议和本机管理员通道，不能当作完整部署验收。
func TestSecuritySetupBrowserFixture(t *testing.T) {
	if os.Getenv("SMAIL_BROWSER_FIXTURE") != "1" {
		t.Skip("browser fixture is opt-in")
	}
	root := os.Getenv("SMAIL_BROWSER_ROOT")
	password := os.Getenv("SMAIL_BROWSER_PASSWORD")
	if root == "" || password == "" {
		t.Fatal("isolated root and password are required")
	}
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	databasePath := filepath.Join(root, "browser.db")
	if _, err := os.Stat(databasePath); !os.IsNotExist(err) {
		t.Fatal("fixture requires a fresh database path")
	}
	config.ROOT_PATH = root
	config.DevelopmentMode = false
	config.Instance = &config.Config{
		DbType: config.DBTypeSQLite, DbDSN: databasePath,
		Domain: "localhost", WebDomain: "localhost", AccessHosts: []string{"127.0.0.1"},
		IsInit: true, SetupState: config.SetupStateReady, HttpsPort: 15443,
	}
	config.SetInitState(true)
	if err := db.Init("browser-fixture"); err != nil {
		t.Fatal(err)
	}
	defer db.Instance.Close()
	user := models.User{Account: "xuans", Name: "Test", SessionVersion: 1, SecurityVersion: 1}
	user.ApplyRole(access.RoleSuperAdmin)
	if err := auth.SetPassword(&user, password); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Instance.Insert(&user); err != nil {
		t.Fatal(err)
	}
	credential, err := credentials.NewService(db.Instance)
	if err != nil {
		t.Fatal(err)
	}
	vault, err := externalemail.NewVault(root)
	if err != nil {
		t.Fatal(err)
	}
	external, err := externalemail.NewRepository(db.Instance, vault)
	if err != nil {
		t.Fatal(err)
	}
	controllers.ConfigureSecurityRuntime(nil, external, nil, credential)
	if err := session.Init(); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	if err := router(mux); err != nil {
		t.Fatal(err)
	}
	frontend := http.FileServer(http.Dir(os.Getenv("SMAIL_BROWSER_DIST")))
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			mux.ServeHTTP(w, r)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		frontend.ServeHTTP(w, r)
	})
	listener, err := net.Listen("tcp", "127.0.0.1:15443")
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewUnstartedServer(securityHeaders(session.Instance.LoadAndSave(handler)))
	_ = server.Listener.Close()
	server.Listener = listener
	server.StartTLS()
	defer server.Close()
	t.Log("isolated HTTPS fixture ready on port 15443")
	select {
	case <-time.After(30 * time.Minute):
	case <-t.Context().Done():
	}
}
