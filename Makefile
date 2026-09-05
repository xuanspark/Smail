SMAIL_VERSION ?= $(shell git describe --tags --always --dirty)
GIT_HASH ?= $(shell git show -s --format=%H)
BUILD_TIME ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
GO_VERSION ?= $(shell go version)
BUILD_LDFLAGS = -s -w -X 'git.pcux.cn/xuans/Smail/internal/buildinfo.Version=$(SMAIL_VERSION)' -X 'git.pcux.cn/xuans/Smail/internal/buildinfo.GitHash=$(GIT_HASH)' -X 'git.pcux.cn/xuans/Smail/internal/buildinfo.BuildTime=$(BUILD_TIME)' -X 'git.pcux.cn/xuans/Smail/internal/buildinfo.GoVersion=$(GO_VERSION)'

.PHONY: build clean clean_codex_test_artifacts build_fe build_server wechat_push spam_block telegram_push plugin package test test_mysql test_postgres

build: build_fe build_server spam_block wechat_push telegram_push package

clean:
	rm -rf output

clean_codex_test_artifacts:
	cd server && git clean -f -x -- .codex-smail-*

build_fe:
	cd fe && yarn && yarn build
	rm -rf server/listen/http_server/dist
	cd server && cp -rf ../fe/dist listen/http_server

build_server:
	cd server && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags "$(BUILD_LDFLAGS)" -o smail_linux_amd64 ./cmd/smail
	cd server && CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -ldflags "$(BUILD_LDFLAGS)" -o smail_linux_arm64 ./cmd/smail
	cd server && CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -ldflags "$(BUILD_LDFLAGS)" -o smail_windows_amd64.exe ./cmd/smail
	cd server && CGO_ENABLED=0 GOOS=windows GOARCH=arm64 go build -ldflags "$(BUILD_LDFLAGS)" -o smail_windows_arm64.exe ./cmd/smail
	cd server && CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build -ldflags "$(BUILD_LDFLAGS)" -o smail_mac_amd64 ./cmd/smail
	cd server && CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -ldflags "$(BUILD_LDFLAGS)" -o smail_mac_arm64 ./cmd/smail
	cd server && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags "$(BUILD_LDFLAGS)" -o smail-admin_linux_amd64 ./cmd/smail-admin
	cd server && CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -ldflags "$(BUILD_LDFLAGS)" -o smail-admin_linux_arm64 ./cmd/smail-admin
	cd server && CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -ldflags "$(BUILD_LDFLAGS)" -o smail-admin_windows_amd64.exe ./cmd/smail-admin
	cd server && CGO_ENABLED=0 GOOS=windows GOARCH=arm64 go build -ldflags "$(BUILD_LDFLAGS)" -o smail-admin_windows_arm64.exe ./cmd/smail-admin
	cd server && CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build -ldflags "$(BUILD_LDFLAGS)" -o smail-admin_mac_amd64 ./cmd/smail-admin
	cd server && CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -ldflags "$(BUILD_LDFLAGS)" -o smail-admin_mac_arm64 ./cmd/smail-admin

wechat_push:
	cd server/hooks/wechat_push && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags "-s -w" -o output/wechat_push_linux_amd64 wechat_push.go
	cd server/hooks/wechat_push && CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -ldflags "-s -w" -o output/wechat_push_linux_arm64 wechat_push.go
	cd server/hooks/wechat_push && CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -ldflags "-s -w" -o output/wechat_push_windows_amd64.exe wechat_push.go
	cd server/hooks/wechat_push && CGO_ENABLED=0 GOOS=windows GOARCH=arm64 go build -ldflags "-s -w" -o output/wechat_push_windows_arm64.exe wechat_push.go
	cd server/hooks/wechat_push && CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build -ldflags "-s -w" -o output/wechat_push_mac_amd64 wechat_push.go
	cd server/hooks/wechat_push && CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -ldflags "-s -w" -o output/wechat_push_mac_arm64 wechat_push.go

spam_block:
	cd server/hooks/spam_block && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags "-s -w" -o output/spam_block_linux_amd64 spam_block.go
	cd server/hooks/spam_block && CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -ldflags "-s -w" -o output/spam_block_linux_arm64 spam_block.go
	cd server/hooks/spam_block && CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -ldflags "-s -w" -o output/spam_block_windows_amd64.exe spam_block.go
	cd server/hooks/spam_block && CGO_ENABLED=0 GOOS=windows GOARCH=arm64 go build -ldflags "-s -w" -o output/spam_block_windows_arm64.exe spam_block.go
	cd server/hooks/spam_block && CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build -ldflags "-s -w" -o output/spam_block_mac_amd64 spam_block.go
	cd server/hooks/spam_block && CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -ldflags "-s -w" -o output/spam_block_mac_arm64 spam_block.go

telegram_push:
	cd plugins/pmail_telegram_push && npm ci && npm run build
	cd plugins/pmail_telegram_push && mkdir -p output && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags "-s -w" -o output/pmail_telegram_push_linux_amd64 .
	cd plugins/pmail_telegram_push && CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -ldflags "-s -w" -o output/pmail_telegram_push_linux_arm64 .
	cd plugins/pmail_telegram_push && CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -ldflags "-s -w" -o output/pmail_telegram_push_windows_amd64.exe .
	cd plugins/pmail_telegram_push && CGO_ENABLED=0 GOOS=windows GOARCH=arm64 go build -ldflags "-s -w" -o output/pmail_telegram_push_windows_arm64.exe .
	cd plugins/pmail_telegram_push && CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build -ldflags "-s -w" -o output/pmail_telegram_push_mac_amd64 .
	cd plugins/pmail_telegram_push && CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -ldflags "-s -w" -o output/pmail_telegram_push_mac_arm64 .

plugin: spam_block wechat_push telegram_push

package: clean
	mkdir output
	mkdir output/config
	mkdir output/plugins
	mv server/smail_linux_amd64 server/smail_linux_arm64 server/smail_windows_amd64.exe server/smail_windows_arm64.exe server/smail_mac_amd64 server/smail_mac_arm64 output/
	mv server/smail-admin_linux_amd64 server/smail-admin_linux_arm64 server/smail-admin_windows_amd64.exe server/smail-admin_windows_arm64.exe server/smail-admin_mac_amd64 server/smail-admin_mac_arm64 output/
	mkdir -p output/config/dkim && cp server/config/dkim/dkim.public output/config/dkim/ 2>/dev/null || true
	cp -r server/config/ssl output/config/
	cp -r server/config/config.json output/config/
	mv server/hooks/spam_block/output/* output/plugins
	mv server/hooks/wechat_push/output/* output/plugins
	mv plugins/pmail_telegram_push/output/* output/plugins
	cp README.md output/
	printf '{\n  "version": "%s",\n  "gitHash": "%s",\n  "protocolVersion": 1,\n  "createdAt": "%s"\n}\n' "$(SMAIL_VERSION)" "$(GIT_HASH)" "$(BUILD_TIME)" > output/release.json

test:
	@trap 'cd "$(CURDIR)/server" && git clean -f -x -- .codex-smail-*' EXIT; export setup_port=17888 && cd server && export SMAIL_ROOT=$(CURDIR)/server/ && go test -v -p 1 ./...

test_mysql:
	@trap 'cd "$(CURDIR)/server" && git clean -f -x -- .codex-smail-*' EXIT; export setup_port=17888 && cd server && export SMAIL_ROOT=$(CURDIR)/server/ && go test -args "mysql" -v -p 1 ./...

test_postgres:
	@trap 'cd "$(CURDIR)/server" && git clean -f -x -- .codex-smail-*' EXIT; export setup_port=17888 && cd server && export SMAIL_ROOT=$(CURDIR)/server/ && go test -args "postgres" -v -p 1 ./...
