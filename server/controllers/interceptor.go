package controllers

import (
	"git.pcux.cn/xuans/Smail/config"
	"net/http"
)

func Interceptor(w http.ResponseWriter, r *http.Request) {
	cfg := config.CurrentConfig()
	if cfg == nil || cfg.WebDomain == "" {
		http.Error(w, "HTTPS is required", http.StatusUpgradeRequired)
		return
	}
	URL := "https://" + cfg.WebDomain + r.URL.Path
	http.Redirect(w, r, URL, http.StatusMovedPermanently)
}
