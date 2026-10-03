//go:build linux

package hooks

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"git.pcux.cn/xuans/Smail/utils/credential"
)

func startPluginEgress(spec *pluginLaunchSpec, uid, gid int) error {
	logicalID := ""
	for _, item := range spec.procAttr.Env {
		if strings.HasPrefix(item, "SMAIL_PLUGIN_ID=") {
			logicalID = strings.TrimPrefix(item, "SMAIL_PLUGIN_ID=")
		}
	}
	if logicalID == "" {
		return errors.New("plugin egress identity is unavailable")
	}
	token, err := newPluginAuthToken()
	if err != nil {
		return errors.New("plugin egress authorization generation failed")
	}
	socketPath := filepath.Join(spec.runtimeDir, filepath.Base(credential.PluginEgressSocket))
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: socketPath, Net: "unix"})
	if err != nil {
		return errors.New("plugin egress listener creation failed")
	}
	if err := os.Chmod(socketPath, 0600); err != nil {
		_ = listener.Close()
		return errors.New("plugin egress socket permissions failed")
	}
	if err := os.Chown(socketPath, uid, gid); err != nil {
		_ = listener.Close()
		return errors.New("plugin egress socket identity failed")
	}
	ctx, cancel := context.WithCancel(context.Background())
	var telegramProxy func() (string, error)
	if logicalID == "telegram_push" {
		telegramProxy = func() (string, error) {
			instance, _, ok := pluginInstanceForStartup(logicalID)
			if !ok || validatePluginRuntimeBinding(logicalID, instance) != nil {
				return "", errors.New("Telegram plugin configuration is unavailable")
			}
			var settings struct {
				Proxy string `json:"proxy"`
			}
			if err := json.Unmarshal(instance.Settings, &settings); err != nil {
				return "", errors.New("Telegram plugin configuration is invalid")
			}
			return credential.NormalizeTelegramProxyURL(settings.Proxy)
		}
	}
	server := &http.Server{
		Handler: credential.NewPluginEgressHandlerWithTelegramProxy(token, func() []credential.NetworkTarget {
			instance, _, ok := pluginInstanceForStartup(logicalID)
			if !ok || validatePluginRuntimeBinding(logicalID, instance) != nil {
				return nil
			}
			canNetwork := false
			for _, capability := range instance.Permissions.Capabilities {
				canNetwork = canNetwork || capability == "network"
			}
			if !canNetwork {
				return nil
			}
			targets := make([]credential.NetworkTarget, 0, len(instance.Permissions.Network))
			for _, target := range instance.Permissions.Network {
				if target.Scheme == "https" {
					targets = append(targets, credential.NetworkTarget{Scheme: target.Scheme, Host: target.Host, Port: target.Port})
				}
			}
			return targets
		}, telegramProxy),
		BaseContext:       func(net.Listener) context.Context { return ctx },
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      65 * time.Second,
		IdleTimeout:       5 * time.Second,
		MaxHeaderBytes:    32 << 10,
		ErrorLog:          log.New(io.Discard, "", 0),
	}
	server.SetKeepAlivesEnabled(false)
	spec.egressStop = func() {
		cancel()
		_ = listener.Close()
		_ = server.Close()
	}
	spec.procAttr.Env = appendPluginEnv(spec.procAttr.Env, "SMAIL_PLUGIN_EGRESS_TOKEN", token)
	go func() { _ = server.Serve(listener) }()
	return nil
}
