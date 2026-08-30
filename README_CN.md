# Smail

> 一台服务器、一个域名、一行代码、一分钟时间，你就能够搭建出一个自己的域名邮箱。

Smail是一个追求极简部署流程、极致资源占用的个人域名邮箱服务器。单文件运行，包含完整的收发邮件服务和Web端邮件管理功能。只需一台服务器、一个域名、一行代码、一分钟部署时间，你就能够搭建出一个自己的域名邮箱。

欢迎各类PR，无论你是修复bug、新增功能、修改翻译。另外，也为这个项目征集一个漂亮可爱的Logo！

<img src="./docs/cn.gif" alt="Editor" width="800px">

## 为什么写这个项目

迫于越来越多的邮件服务商暂停了针对个人的域名邮箱服务（比如QQ邮箱、微软Outlook邮箱），因此考虑自建域名邮箱服务。
但是自建域名邮箱可选的程序并不多，且目标都不是针对个人使用场景设计的。个人服务器一般内存、CPU、硬盘配置都不高，针对公司场景使用的邮箱程序过于臃肿，
白白浪费资源。就拿我自己的服务器来说，我服务器配置为1核512M 10G硬盘，市面上绝大多数邮箱服务器安装上就把磁盘占满了，根本没法正常使用

## 项目优势

### 1、部署简单

使用Go语言编写，支持跨平台，编译后单文件运行，单文件包含完整的前后端代码。修改配置文件，运行即可。

### 2、资源占用极小

编译后二进制文件仅15MB，运行过程中占用内存10M以内。

### 3、安全方面

支持dkim、spf校验。正确配置的情况下，Email Test得分10分。

### 4、自动SSL证书

实现了ACME协议，程序将自动获取并更新Let’s Encrypt证书。

默认情况下，会为web后台也生成ssl证书，让后台使用https访问，如果你有自己的网关层，不需要https的话，在配置文件中将 `httpsEnabled`
设置为 `2`，这样管理后台就不会使用https协议。（ 注意：即使你不需要https，也请保证ssl证书文件路径正确，http协议虽然不使用证书了，但是smtp协议还需要证书）

### 5、邮件客户端支持

只要支持pop3、smtp、imap协议的邮件客户端均可使用

### 6、多域名、多用户支持

支持多域名、多用户且完整支持收发邮件

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

Smail 的 IMAP、POP3、SMTP 第三方客户端登录默认受用户级安全设置保护。每个协议都必须分别完成以下步骤：

1. 登录 Web 管理页面，打开“设置 → 账户安全 → 第三方邮件客户端登录”。
2. 开启需要使用的协议开关。
3. 为该协议生成授权码，并在生成后立即复制保存。授权码只在生成成功时显示一次，重新生成会使同一协议的旧授权码失效。
4. 在邮件客户端中，登录名必须填写完整邮箱地址，例如 `alice@example.com`，不能只填写 `alice`。
5. 客户端密码必须填写对应协议的授权码，不能填写网页登录密码、离线恢复码或邮箱验证码。

## 服务地址和端口

服务地址通常使用已解析到 Smail 服务器的邮件子域名。请根据实际 DNS 配置填写，例如 `imap.example.com`、`pop.example.com` 和 `smtp.example.com`。如果所有协议共用同一个邮件域名，也可以使用实际部署的统一主机名；不要直接照抄示例域名。

| 协议 | 服务地址示例 | 推荐端口 | 加密方式 | 登录名 | 密码 |
| --- | --- | --- | --- | --- | --- |
| IMAP | `imap.example.com` | `993` | SSL/TLS | 完整邮箱地址 | IMAP 授权码 |
| POP3 | `pop.example.com` | `995` | SSL/TLS | 完整邮箱地址 | POP3 授权码 |
| SMTP | `smtp.example.com` | `465` | SSL/TLS | 完整邮箱地址 | SMTP 授权码 |
| SMTP | `smtp.example.com` | `587` | STARTTLS | 完整邮箱地址 | SMTP 授权码 |
| SMTP | `smtp.example.com` | `25` | 明文或按服务器策略启用 TLS | 完整邮箱地址 | SMTP 授权码 |

端口说明：

- `993` 和 `995` 分别用于 IMAP over SSL/TLS 和 POP3 over SSL/TLS，生产环境优先使用这两个端口。
- SMTP `465` 使用连接即加密的 SSL/TLS；SMTP `587` 使用 STARTTLS，客户端必须打开“STARTTLS”或“需要安全连接”选项。
- SMTP `25` 主要用于服务器间投递，许多云服务商会限制出站连接；普通客户端提交邮件优先使用 `465` 或 `587`。
- Web 页面由 Nginx 代理 HTTPS 时，不代表 IMAP、POP3、SMTP 端口也由 Nginx 代理。邮件客户端仍应连接邮件服务实际监听的端口，并使用邮件服务端配置的证书和 TLS 设置。

## 协议开关和授权码规则

- 关闭协议后，该用户对应协议的第三方登录立即失效，网页登录不受影响。
- 撤销授权码后，该协议已有客户端会在下一次认证时失败；需要重新生成授权码并更新客户端配置。
- 开启协议但尚未生成授权码时，协议仍不能登录。
- 授权码按协议分别管理，IMAP、POP3、SMTP 可以使用不同授权码；不要把一个协议的授权码填到另一个协议。
- 授权码只保存哈希，不会在设置页面再次读取，也不会写入日志。请将授权码视为长期密码，不要提交到工单、截图或 Git。
- Smail 第三方协议认证不会接受网页登录密码。这样可以在不暴露网页登录密码的情况下单独撤销邮件客户端访问。

## 常见登录失败排查

1. 确认协议开关已开启，并且该协议已经生成授权码。
2. 确认登录名包含完整域名，例如 `alice@example.com`，不要填写 `alice`、显示名称或别名。
3. 确认密码是当前协议的授权码，不是网页登录密码、恢复码或邮箱验证码。
4. 确认客户端加密模式和端口匹配：`465` 使用 SSL/TLS，`587` 使用 STARTTLS，`993/995` 使用 SSL/TLS。
5. 确认邮件协议主机名的 DNS 记录指向实际 Smail 服务器，防火墙已放行对应端口，证书的域名与客户端连接的主机名匹配。
6. 如果刚刚关闭协议、撤销或重新生成授权码，请删除客户端保存的旧密码后重新输入新授权码。
7. 仅检查 Web HTTPS 是否正常不能证明邮件协议可用；应分别使用 SMTP、IMAP 或 POP3 客户端进行认证测试。

## PMail 原版兼容性说明

PMail 原版的协议登录流程与 Smail 不兼容，不能直接作为 Smail 的第三方客户端兼容层：

- PMail 原版会把包含 `@` 的登录名截断为账号部分，不能可靠识别 Smail 要求的完整邮箱地址。
- PMail 原版没有按用户、按协议的开关和授权码机制。
- PMail 原版把客户端密码当作网页登录密码校验；Smail 要求客户端使用协议授权码，因此把 Smail 授权码填入 PMail 原版会被当成错误的网页登录密码。
- Smail 使用 Argon2id 保存用户密码，PMail 原版的旧密码校验逻辑不能直接验证 Smail 的 Argon2id 哈希。
- 多域名环境下，PMail 截断域名还可能造成账号冲突或错误用户解析。

因此，使用 Smail 邮件服务时，请让客户端直接连接 Smail 的 SMTP、IMAP、POP3 服务，并按本节的开关、完整邮箱地址和协议授权码规则配置；不要把 PMail 原版的账号截断或旧密码逻辑复制到 Smail。
# 插件

[微信推送](server/hooks/wechat_push/README.md)

[垃圾邮件屏蔽](server/hooks/spam_block/README.md)

# 其他优秀开源插件

[Telegram推送](https://github.com/ydzydzydz/pmail_telegram_push)

## 插件安装
> [!IMPORTANT]
> 插件已独立进程的方式运行在你的服务器上，请自行审查第三方插件的安全性。Smail目前仅维护上述三款插件

将插件二进制文件放到`plugins`文件夹中即可

# 参与开发

## 项目架构

1、前端： vue3+element-plus

前端代码位于 `fe`目录中，运行参考 `fe`目录中的README文件

2、后端： golang + MySQL/SQLite

后端代码进入 `server`文件夹，运行 `main.go`文件

3、编译项目

`make build`

4、单元测试

`make test`

## 后端接口文档

[go to wiki](https://git.pcux.cn/xuans/Smail/wiki/%E5%90%8E%E7%AB%AF%E6%8E%A5%E5%8F%A3%E6%96%87%E6%A1%A3)

## 插件开发

[go to wiki](https://git.pcux.cn/xuans/Smail/wiki/%E6%8F%92%E4%BB%B6%E5%BC%80%E5%8F%91%E8%AF%B4%E6%98%8E)


