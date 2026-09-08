//go:build bundled

package main

import (
	"embed"
	"io/fs"

	"git.pcux.cn/xuans/Smail/hooks"
)

//go:embed bundled/*
var pluginFiles embed.FS

func init() {
	files, err := fs.Sub(pluginFiles, "bundled")
	if err != nil {
		panic(err)
	}
	hooks.SetBundledPlugins(files)
}
