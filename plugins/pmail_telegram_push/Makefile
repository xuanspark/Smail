.PHONY: build

build:
	pnpm install
	pnpm build
	go mod tidy
	mkdir -p ../../out/plugins
	go build -o ../../out/plugins/pmail_telegram_push -ldflags "-s -w"

install:
	mkdir -p ./config ./plugins
	cp -v ../../out/plugins/pmail_telegram_push ./plugins/pmail_telegram_push
	cp -v config/pmail_telegram_push.json.example ./config/pmail_telegram_push.json

clean:
	@echo "请先审查 out/plugins 再清理，保留已安装的插件与配置。"
