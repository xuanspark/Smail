# Smail

当前版本每套部署只使用一个数据库，SQLite、MySQL、PostgreSQL 三选一，配置历史与业务数据共用所选数据库，不再新建独立的 SQLite 状态库。生效配置保存在根目录 `config` 的五个 JSON 文件中；新部署的密钥和证书保存在 `ssl`，SQLite 文件及配置恢复事务日志保存在 `data`。请阅读 [单数据库部署说明](docs/单数据库部署说明.md) 和 [运行目录迁移说明](docs/运行目录迁移说明.md)，升级前停服并完整备份；不能只复制配置文件，也不能沿用下文历史容器示例中仅挂载 `config` 的方式。

> 一台服务器、一个域名、一行代码、一分钟时间，你就能够搭建出一个自己的域名邮箱。

Smail是一个追求极简部署流程、极致资源占用的个人域名邮箱服务器。单文件运行，包含完整的收发邮件服务和Web端邮件管理功能。只需一台服务器、一个域名、一行代码、一分钟部署时间，你就能够搭建出一个自己的域名邮箱。

欢迎各类PR，无论你是修复bug、新增功能、修改翻译。另外，也为这个项目征集一个漂亮可爱的Logo！

## 项目优势与核心特性

### 1. 架构级单数据库统一管理（SQLite / MySQL / PostgreSQL 三选一）
- 彻底消除原版分散的数据库机制，业务数据、配置历史与系统日志共用同一个选定数据库。
- 既支持即开即用的本地嵌入式 SQLite，也原生支持高并发的 MySQL 8.x 与 PostgreSQL 14+，自由切换。

### 2. 极致资源占用与极速部署
- 采用 Go 语言结合现代化 Vue 3 构建，后端直接静态内嵌打包全部前端资源。
- 编译后单二进制文件自包含运行，启动极快，常规运行内存仅在 20MB 以内，特别适用于 1核512M 等高性价比轻量云主机。
- 配套一键式自动化安装脚本（install-systemd.sh）与在线平滑更新脚本（update-systemd.sh），自动配置防火墙规则并开机自启。

### 3. 企业级安全审计与多因素认证 (2FA)
- 支持 **TOTP 动态验证码** 与 **Passkey / WebAuthn 通行密钥**，提供工业级身份验证保障。
- 具备超管本地 IPC 隔离重置通道、自动化防暴力破解保护、全库敏感凭据哈希加密存储。
- 通过严格的代码安全性验证（全面通过 gosec 安全扫描与 govulncheck 漏洞检测），杜绝高并发内存竞态（Data Race）。

### 4. 满分邮件送达率信誉（10/10 评分）
- 严格遵循现代邮件反垃圾标准，内置 **DKIM** 签名机制、**SPF** 匹配校验以及 **DMARC** 报告分析与对准策略。
- 规范支持 **List-Unsubscribe** 标头，降低被主流邮件服务商（如 Gmail、QQ、网易等）拦截或标记为垃圾邮件的概率。
- 具备可视化外部 SMTP 智能中继，配备独立启用开关与管理列表，即使服务器 25 端口受限也能顺畅发信。

### 5. 全自动 SSL 证书全生命周期管理
- 深度集成 ACME 自动化协议，支持 HTTP-01 及主流 DNS API（阿里云 DNS、腾讯云 DNS 等）自动申请与续签 Let's Encrypt 证书。
- 自动涵盖主域名与通配符泛域名，同时支持直接导入并热更新外部自定义证书。

### 6. 现代自适应 Web 管理与全功能工作台
- 采用全新响应式双栏布局，自适应匹配桌面宽屏、平板与手机浏览器。
- 内嵌免配置即时多语言翻译、富文本邮件编辑器、快速导出安全码以及多域名/多用户收发控制系统。

### 7. 全平台标准邮件客户端完全兼容
- 完整实现标准的 POP3 (110/995)、SMTP (25/465/587) 与 IMAP (143/993) 协议。
- 完美兼容各类主流客户端（Foxmail、Outlook、Thunderbird、Apple Mail 及各类手机自带邮件 App）。

# 如何部署

## 0、检查IP、域名

先去[spamhaus](https://check.spamhaus.org/)检查你的域名和服务器IP是否有屏蔽记录

## 1、下载文件

* [点击这里](https://git.pcux.cn/xuans/Smail/releases)下载一个与你匹配的程序文件。
* 或者使用Docker运行 `docker pull ghcr.io/jinnrry/pmail:latest`

## 2、运行

`./smail -p 80`

> `-p 指定引导设置界面的http端口，默认为80端口，注意该参数仅影响引导设置阶段，设置完成后如果需要修改端口请修改配置文件`

> [!IMPORTANT]
> 如果引导设置阶段使用非80端口，将无法自动设置SSL证书

或者

`docker run -p 25:25 -p 80:80 -p 443:443 -p 110:110 -p 465:465 -p 587:587 -p 995:995 -p 993:993 -v $(pwd)/config:/work/config ghcr.io/jinnrry/pmail:latest`

> [!IMPORTANT]
> 如果你服务器开启了防火墙，你需要打开25、80、110、443、465、587、995、993端口

## 3、配置

浏览器打开 `http://127.0.0.1` 或者是用你服务器公网IP访问，然后按提示配置

## 4、邮箱得分测试

建议找一下邮箱测试服务(比如[https://www.mail-tester.com/](https://www.mail-tester.com/))进行邮件得分检测，避免自己某些步骤漏配，导致发件进对方垃圾箱。

# 配置文件说明

以下是旧版单文件配置示例，仅供迁移时识别字段，不能覆盖当前五分区配置。当前文件为 `config/config.json`、`config/smtp.json`、`config/email.json`、`config/ssl.json` 和 `config/plugins.json`；建议通过管理页面修改，系统会同步维护代次与摘要。

```jsonc
{
  "logLevel": "info", //日志输出级别
  "domain": "domain.com", // 你的域名
  "webDomain": "mail.domain.com", // web域名
  "dkimPrivateKeyPath": "config/dkim/dkim.priv", // dkim 私钥地址
  "sslType": "0", // ssl证书更新模式，0自动，HTTP模式，1手动、2自动，DNS模式
  "SSLPrivateKeyPath": "config/ssl/private.key", // ssl 证书地址
  "SSLPublicKeyPath": "config/ssl/public.crt", // ssl 证书地址
  "dbDSN": "./config/smail.db", // 数据库连接DSN
  "dbType": "sqlite", //数据库类型，支持sqlite 和 mysql
  "httpsEnabled": 0, // web后台是否启用https 0默认（启用），1启用，2不启用
  "spamFilterLevel": 0,// 垃圾邮件过滤级别，0不过滤、1 spf dkim 校验均失败时且无有效收件人过滤，2 spf校验不通过时且无有效收件人过滤 3,dkim 校验不过的时候且无有效收件人过滤
  "httpPort": 80, // http 端口 . 默认 80
  "httpsPort": 443, // https 端口 . 默认 443
  "isInit": true // 为false的时候会进入安装引导流程
}
```

# 第三方邮件客户端配置

POP3地址： pop.[你的域名]

POP3端口： 110/995(SSL)

SMTP地址： smtp.[你的域名]

SMTP端口： 25/465、587(SSL)

IMAP地址： imap.[Your Domain]

IMAP端口： 993(SSL)

# 插件

[微信推送](backend/hooks/wechat_push/README.md)

[垃圾邮件屏蔽](backend/hooks/spam_block/README.md)

# 其他优秀开源插件

[Telegram推送](https://github.com/ydzydzydz/pmail_telegram_push)

## 插件安装
> [!IMPORTANT]
> 插件已独立进程的方式运行在你的服务器上，请自行审查第三方插件的安全性。Smail目前仅维护上述三款插件

将插件二进制文件放到`plugins`文件夹中即可

# 参与开发

目录约定：独立测试代码和脚本位于 `test/`，测试日志及截图位于 `test-log/`，编译输出位于 `out/`。构建与开发启动方式见 [文件目录整理说明](docs/文件目录整理说明.md)，测试入口见 [测试说明](test/README.md)。

## 项目架构

1、前端： vue3+element-plus

前端代码位于 `frontend` 目录中，运行参考 `frontend` 目录中的 README 文件。

2、后端： golang + MySQL/SQLite

后端代码位于 `backend/`；推荐使用 `deploy/build.ps1` 构建后，通过 `deploy/start-dev.ps1` 启动开发服务。

3、编译项目

`make build`

4、单元测试

`make test`

## 后端接口文档

[go to wiki](https://git.pcux.cn/xuans/Smail/wiki/%E5%90%8E%E7%AB%AF%E6%8E%A5%E5%8F%A3%E6%96%87%E6%A1%A3)

## 插件开发

[go to wiki](https://git.pcux.cn/xuans/Smail/wiki/%E6%8F%92%E4%BB%B6%E5%BC%80%E5%8F%91%E8%AF%B4%E6%98%8E)



## Releases & CI/CD Workflows

The project maintains automated build & release workflows across both repositories:
- **GitHub**: [https://github.com/xuanspark/Smail.git](https://github.com/xuanspark/Smail.git)
- **PCUX Git**: [https://git.pcux.cn/xuans/Smail.git](https://git.pcux.cn/xuans/Smail.git)

Automated multi-platform release packages (containing full frontend & embedded assets):
- **Linux**: Smail_linux_amd64.tar.gz / Smail_linux_arm64.tar.gz
- **Windows**: Smail_windows_amd64.zip / Smail_windows_arm64.zip
- **macOS**: Smail_darwin_amd64.tar.gz / Smail_darwin_arm64.tar.gz
