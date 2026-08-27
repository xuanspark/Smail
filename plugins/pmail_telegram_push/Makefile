.PHONY: build

build:
	pnpm install
	pnpm build
	go mod tidy
	go build -o pmail_telegram_push -ldflags "-s -w"

install:
	mkdir -p ./config ./plugins
	cp -v pmail_telegram_push ./plugins/pmail_telegram_push
	cp -v config/pmail_telegram_push.json.example ./config/pmail_telegram_push.json

clean:
	rm -vf pmail_telegram_push ./plugins/pmail_telegram_push ./config/pmail_telegram_push.json
