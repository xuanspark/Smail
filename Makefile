SMAIL_VERSION ?= $(shell git describe --tags --always --dirty)
GIT_HASH ?= $(shell git show -s --format=%H)
BUILD_TIME ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
GO_VERSION ?= $(shell go version)
BUILD_LDFLAGS = -s -w -X 'git.pcux.cn/xuans/Smail/internal/buildinfo.Version=$(SMAIL_VERSION)' -X 'git.pcux.cn/xuans/Smail/internal/buildinfo.GitHash=$(GIT_HASH)' -X 'git.pcux.cn/xuans/Smail/internal/buildinfo.BuildTime=$(BUILD_TIME)' -X 'git.pcux.cn/xuans/Smail/internal/buildinfo.GoVersion=$(GO_VERSION)'

.PHONY: build clean clean_codex_test_artifacts build_fe build_server wechat_push spam_block telegram_push plugin package test test_mysql test_postgres

build: build_fe build_server spam_block wechat_push telegram_push package

clean:
	@echo "请先审查 test-log 中的清理清单，不自动递归删除文件。"

clean_codex_test_artifacts:
	@echo "测试产物保留在 test-log 中，清理前请先审查。"

build_fe:
	cd frontend && yarn && yarn build

build_server:
	mkdir -p out/backend
	cd backend && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags "$(BUILD_LDFLAGS)" -o ../out/backend/smail_linux_amd64 ./cmd/smail
	cd backend && CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -ldflags "$(BUILD_LDFLAGS)" -o ../out/backend/smail_linux_arm64 ./cmd/smail
	cd backend && CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -ldflags "$(BUILD_LDFLAGS)" -o ../out/backend/smail_windows_amd64.exe ./cmd/smail
	cd backend && CGO_ENABLED=0 GOOS=windows GOARCH=arm64 go build -ldflags "$(BUILD_LDFLAGS)" -o ../out/backend/smail_windows_arm64.exe ./cmd/smail
	cd backend && CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build -ldflags "$(BUILD_LDFLAGS)" -o ../out/backend/smail_mac_amd64 ./cmd/smail
	cd backend && CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -ldflags "$(BUILD_LDFLAGS)" -o ../out/backend/smail_mac_arm64 ./cmd/smail
	cd backend && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags "$(BUILD_LDFLAGS)" -o ../out/backend/smail-admin_linux_amd64 ./cmd/smail-admin
	cd backend && CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -ldflags "$(BUILD_LDFLAGS)" -o ../out/backend/smail-admin_linux_arm64 ./cmd/smail-admin
	cd backend && CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -ldflags "$(BUILD_LDFLAGS)" -o ../out/backend/smail-admin_windows_amd64.exe ./cmd/smail-admin
	cd backend && CGO_ENABLED=0 GOOS=windows GOARCH=arm64 go build -ldflags "$(BUILD_LDFLAGS)" -o ../out/backend/smail-admin_windows_arm64.exe ./cmd/smail-admin
	cd backend && CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build -ldflags "$(BUILD_LDFLAGS)" -o ../out/backend/smail-admin_mac_amd64 ./cmd/smail-admin
	cd backend && CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -ldflags "$(BUILD_LDFLAGS)" -o ../out/backend/smail-admin_mac_arm64 ./cmd/smail-admin

wechat_push:
	mkdir -p out/plugins
	cd backend/hooks/wechat_push && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags "-s -w" -o ../../../out/plugins/wechat_push_linux_amd64 wechat_push.go
	cd backend/hooks/wechat_push && CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -ldflags "-s -w" -o ../../../out/plugins/wechat_push_linux_arm64 wechat_push.go
	cd backend/hooks/wechat_push && CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -ldflags "-s -w" -o ../../../out/plugins/wechat_push_windows_amd64.exe wechat_push.go
	cd backend/hooks/wechat_push && CGO_ENABLED=0 GOOS=windows GOARCH=arm64 go build -ldflags "-s -w" -o ../../../out/plugins/wechat_push_windows_arm64.exe wechat_push.go
	cd backend/hooks/wechat_push && CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build -ldflags "-s -w" -o ../../../out/plugins/wechat_push_mac_amd64 wechat_push.go
	cd backend/hooks/wechat_push && CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -ldflags "-s -w" -o ../../../out/plugins/wechat_push_mac_arm64 wechat_push.go

spam_block:
	mkdir -p out/plugins
	cd backend/hooks/spam_block && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags "-s -w" -o ../../../out/plugins/spam_block_linux_amd64 spam_block.go
	cd backend/hooks/spam_block && CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -ldflags "-s -w" -o ../../../out/plugins/spam_block_linux_arm64 spam_block.go
	cd backend/hooks/spam_block && CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -ldflags "-s -w" -o ../../../out/plugins/spam_block_windows_amd64.exe spam_block.go
	cd backend/hooks/spam_block && CGO_ENABLED=0 GOOS=windows GOARCH=arm64 go build -ldflags "-s -w" -o ../../../out/plugins/spam_block_windows_arm64.exe spam_block.go
	cd backend/hooks/spam_block && CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build -ldflags "-s -w" -o ../../../out/plugins/spam_block_mac_amd64 spam_block.go
	cd backend/hooks/spam_block && CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -ldflags "-s -w" -o ../../../out/plugins/spam_block_mac_arm64 spam_block.go

telegram_push:
	mkdir -p out/plugins
	cd plugins/pmail_telegram_push && npm ci && npm run build
	cd plugins/pmail_telegram_push && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags "-s -w" -o ../../out/plugins/pmail_telegram_push_linux_amd64 .
	cd plugins/pmail_telegram_push && CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -ldflags "-s -w" -o ../../out/plugins/pmail_telegram_push_linux_arm64 .
	cd plugins/pmail_telegram_push && CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -ldflags "-s -w" -o ../../out/plugins/pmail_telegram_push_windows_amd64.exe .
	cd plugins/pmail_telegram_push && CGO_ENABLED=0 GOOS=windows GOARCH=arm64 go build -ldflags "-s -w" -o ../../out/plugins/pmail_telegram_push_windows_arm64.exe .
	cd plugins/pmail_telegram_push && CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build -ldflags "-s -w" -o ../../out/plugins/pmail_telegram_push_mac_amd64 .
	cd plugins/pmail_telegram_push && CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -ldflags "-s -w" -o ../../out/plugins/pmail_telegram_push_mac_arm64 .

plugin: spam_block wechat_push telegram_push

package:
	mkdir -p out/package/config out/package/plugins
	cp out/backend/smail* out/package/
	cp out/plugins/*_* out/package/plugins/
	cp README.md out/package/
	printf '{\n  "version": "%s",\n  "gitHash": "%s",\n  "protocolVersion": 1,\n  "createdAt": "%s"\n}\n' "$(SMAIL_VERSION)" "$(GIT_HASH)" "$(BUILD_TIME)" > out/package/release.json

test:
	node test/run.mjs all

test_mysql:
	node test/run.mjs backend -v -p 1 ./... -args mysql

test_postgres:
	node test/run.mjs backend -v -p 1 ./... -args postgres
