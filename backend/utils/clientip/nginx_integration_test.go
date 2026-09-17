package clientip

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// 此测试仅在远程隔离网络运行，不读取宿主机配置或连接生产应用。
func TestResolveThroughNginx(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("真实 Nginx 验收仅在 Linux 隔离环境执行")
	}
	nginx, err := exec.LookPath("nginx")
	if err != nil {
		t.Skip("当前环境未安装 Nginx，不能声明真实代理验收通过")
	}
	directory, err := os.MkdirTemp("", "smail-clientip-nginx-")
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("隔离 Nginx 目录：%s", directory)
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"client": Resolve(request, []string{"127.0.0.1/32"}),
			"peer":   normalizePeer(request.RemoteAddr),
		})
	}))
	defer backend.Close()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	_ = listener.Close()
	configuration := fmt.Sprintf(`daemon off;
master_process off;
pid %s/nginx.pid;
error_log stderr;
events { worker_connections 64; }
http {
    access_log off;
    client_body_temp_path %s/body;
    proxy_temp_path %s/proxy;
    fastcgi_temp_path %s/fastcgi;
    uwsgi_temp_path %s/uwsgi;
    scgi_temp_path %s/scgi;
    server {
        listen %s;
        location / {
            proxy_pass %s;
            proxy_set_header X-Forwarded-For $remote_addr;
            proxy_set_header X-Real-IP $remote_addr;
        }
    }
}
`, directory, directory, directory, directory, directory, directory, address, backend.URL)
	path := filepath.Join(directory, "nginx.conf")
	if err := os.WriteFile(path, []byte(configuration), 0600); err != nil {
		t.Fatal(err)
	}
	logFile, err := os.Create(filepath.Join(directory, "nginx.log"))
	if err != nil {
		t.Fatal(err)
	}
	defer logFile.Close()
	command := exec.Command(nginx, "-p", directory+"/", "-c", path)
	command.Stdout, command.Stderr = logFile, logFile
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = command.Process.Kill()
		_ = command.Wait()
	}()
	deadline := time.Now().Add(5 * time.Second)
	for {
		connection, err := net.DialTimeout("tcp", address, 100*time.Millisecond)
		if err == nil {
			_ = connection.Close()
			break
		}
		if time.Now().After(deadline) {
			output, _ := os.ReadFile(logFile.Name())
			t.Fatalf("隔离 Nginx 未就绪：%s", output)
		}
		time.Sleep(25 * time.Millisecond)
	}
	for _, source := range []string{"127.0.0.2", "127.0.0.3"} {
		t.Run(source, func(t *testing.T) {
			dialer := &net.Dialer{LocalAddr: &net.TCPAddr{IP: net.ParseIP(source)}, Timeout: time.Second}
			transport := &http.Transport{DialContext: dialer.DialContext, DisableKeepAlives: true}
			defer transport.CloseIdleConnections()
			client := &http.Client{Transport: transport, Timeout: 3 * time.Second}
			request, err := http.NewRequest(http.MethodGet, "http://"+address+"/", nil)
			if err != nil {
				t.Fatal(err)
			}
			request.Header.Add("X-Forwarded-For", "198.51.100.5")
			request.Header.Add("X-Forwarded-For", "203.0.113.8")
			request.Header.Set("X-Real-IP", "198.51.100.6")
			request.Header.Set("Forwarded", "for=198.51.100.7")
			response, err := client.Do(request)
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			var result map[string]string
			if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
				t.Fatal(err)
			}
			if response.StatusCode != http.StatusOK || result["client"] != source || result["peer"] != "127.0.0.1" {
				t.Fatalf("真实代理地址校验失败：状态=%d，结果=%v", response.StatusCode, result)
			}
		})
	}
}
