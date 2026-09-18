SMAIL_VERSION ?= $(shell git describe --tags --always --dirty)
GIT_HASH ?= $(shell git show -s --format=%H)
BUILD_TIME ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
GO_VERSION ?= $(shell cd backend && go version)
BUILD_LDFLAGS = -s -w -X 'git.pcux.cn/xuans/Smail/internal/buildinfo.Version=$(SMAIL_VERSION)' -X 'git.pcux.cn/xuans/Smail/internal/buildinfo.GitHash=$(GIT_HASH)' -X 'git.pcux.cn/xuans/Smail/internal/buildinfo.BuildTime=$(BUILD_TIME)' -X 'git.pcux.cn/xuans/Smail/internal/buildinfo.GoVersion=$(GO_VERSION)'

.PHONY: build clean clean_codex_test_artifacts build_fe build_server wechat_push spam_block telegram_push plugin package test test_mysql test_postgres

build: package

clean:
	@echo "请先审查 test-log 中的清理清单，不自动递归删除文件。"

clean_codex_test_artifacts:
	@echo "测试产物保留在 test-log 中，清理前请先审查。"

build_fe:
	cd frontend && yarn && yarn build

build_server: build_fe plugin
	mkdir -p out/backend
	node deploy/bundle-plugins.mjs linux_amd64
	cd backend && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -tags bundled -trimpath -ldflags "$(BUILD_LDFLAGS)" -o ../out/backend/smail_linux_amd64 ./cmd/smail
	node deploy/bundle-plugins.mjs linux_arm64
	cd backend && CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -tags bundled -trimpath -ldflags "$(BUILD_LDFLAGS)" -o ../out/backend/smail_linux_arm64 ./cmd/smail
	node deploy/bundle-plugins.mjs windows_amd64
	cd backend && CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -tags bundled -trimpath -ldflags "$(BUILD_LDFLAGS)" -o ../out/backend/smail_windows_amd64.exe ./cmd/smail
	node deploy/bundle-plugins.mjs windows_arm64
	cd backend && CGO_ENABLED=0 GOOS=windows GOARCH=arm64 go build -tags bundled -trimpath -ldflags "$(BUILD_LDFLAGS)" -o ../out/backend/smail_windows_arm64.exe ./cmd/smail
	node deploy/bundle-plugins.mjs darwin_amd64
	cd backend && CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build -tags bundled -trimpath -ldflags "$(BUILD_LDFLAGS)" -o ../out/backend/smail_mac_amd64 ./cmd/smail
	node deploy/bundle-plugins.mjs darwin_arm64
	cd backend && CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -tags bundled -trimpath -ldflags "$(BUILD_LDFLAGS)" -o ../out/backend/smail_mac_arm64 ./cmd/smail
	cd backend && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "$(BUILD_LDFLAGS)" -o ../out/backend/smail-admin_linux_amd64 ./cmd/smail-admin
	cd backend && CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -ldflags "$(BUILD_LDFLAGS)" -o ../out/backend/smail-admin_linux_arm64 ./cmd/smail-admin
	cd backend && CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -trimpath -ldflags "$(BUILD_LDFLAGS)" -o ../out/backend/smail-admin_windows_amd64.exe ./cmd/smail-admin
	cd backend && CGO_ENABLED=0 GOOS=windows GOARCH=arm64 go build -trimpath -ldflags "$(BUILD_LDFLAGS)" -o ../out/backend/smail-admin_windows_arm64.exe ./cmd/smail-admin
	cd backend && CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build -trimpath -ldflags "$(BUILD_LDFLAGS)" -o ../out/backend/smail-admin_mac_amd64 ./cmd/smail-admin
	cd backend && CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -trimpath -ldflags "$(BUILD_LDFLAGS)" -o ../out/backend/smail-admin_mac_arm64 ./cmd/smail-admin
	cd backend && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "$(BUILD_LDFLAGS)" -o ../out/backend/smail-config_linux_amd64 ./cmd/smail-config
	cd backend && CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -ldflags "$(BUILD_LDFLAGS)" -o ../out/backend/smail-config_linux_arm64 ./cmd/smail-config
	cd backend && CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -trimpath -ldflags "$(BUILD_LDFLAGS)" -o ../out/backend/smail-config_windows_amd64.exe ./cmd/smail-config
	cd backend && CGO_ENABLED=0 GOOS=windows GOARCH=arm64 go build -trimpath -ldflags "$(BUILD_LDFLAGS)" -o ../out/backend/smail-config_windows_arm64.exe ./cmd/smail-config
	cd backend && CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build -trimpath -ldflags "$(BUILD_LDFLAGS)" -o ../out/backend/smail-config_mac_amd64 ./cmd/smail-config
	cd backend && CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -trimpath -ldflags "$(BUILD_LDFLAGS)" -o ../out/backend/smail-config_mac_arm64 ./cmd/smail-config

wechat_push:
	node deploy/build-plugins.mjs --all --plugin=wechat_push

spam_block:
	node deploy/build-plugins.mjs --all --plugin=spam_block

telegram_push:
	cd plugins/pmail_telegram_push && npm ci && npm run type-check
	node deploy/build-plugins.mjs --all --plugin=telegram_push

plugin: spam_block wechat_push telegram_push

package: build_server plugin
	SMAIL_VERSION="$(SMAIL_VERSION)" GIT_HASH="$(GIT_HASH)" node deploy/package.mjs

test:
	node test/run.mjs all

test_mysql:
	node test/run.mjs backend -v -p 1 ./... -args mysql

test_postgres:
	node test/run.mjs backend -v -p 1 ./... -args postgres
