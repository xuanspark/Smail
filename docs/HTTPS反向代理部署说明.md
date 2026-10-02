# HTTPS 反向代理部署说明

## 适用场景

当 Nginx 已经负责公网 HTTPS 证书、TLS 握手和 443 端口监听时，Smail 不应再次监听 HTTPS 端口。Smail 运行在本机 HTTP 端口，由 Nginx 将 HTTPS 请求转发到 Smail。

本模式用于解决“PMail HTTPS 与 Nginx HTTPS 同时启用后无法访问”的端口冲突、重复跳转和请求来源校验问题。

## Smail 配置

在分区配置的 `config.json` 中配置 HTTP 服务。下面示例仅用于开发测试，测试域名使用 `xuans.com`；不要把示例域名直接当作生产配置。

```json
{
  "domains": {
    "web": "xuans.com"
  },
  "http": {
    "port": 15184,
    "httpsPort": 0,
    "httpsEnabled": false,
    "tlsMode": "proxy",
    "trustedProxyCidrs": [
      "127.0.0.1/32",
      "::1/128"
    ]
  }
}
```

配置含义：

- `tlsMode: proxy`：启用反向代理 HTTPS 终止模式，同时停止 Smail 的自动证书申请和续期。
- `httpsEnabled: false`：Smail 不启动内部 HTTPS。
- `httpsPort: 0`：避免遗留 HTTPS 端口配置造成误解或端口冲突。
- `trustedProxyCidrs`：只允许这些来源提交 `X-Forwarded-Proto`。Nginx 与 Smail 同机时，默认使用回环地址即可。
- 不要把 `0.0.0.0/0` 或 `::/0` 配置为可信代理网段。

修改配置后需要重启 Smail，使监听模式生效。

## 证书申请与续期职责

- Web HTTPS 的证书申请、续期和加载由反代服务器上的证书管理工具负责，Smail 不再发起 ACME 请求，也不需要把 ACME 验证路径转发给 Smail。
- 反代模式下，初始化 SSL 步骤只提供外部证书导入，不再提供自动 HTTP 或 DNS 验证。仅切换反代模式不会删除已有证书或自动模式配置；切回直接 HTTPS 时，仍保留自动类型的证书可继续由 Smail 维护。若已主动导入外部证书，则仍由外部维护，不会自动恢复签发。
- HTTP 反代不等于邮件协议代理。SMTP、IMAP、POP3 仍需使用有效的本地证书和匹配私钥，不能改成明文认证。
- 手动配置可以直接引用服务器上的证书和私钥路径，不生成副本。Nginx 与 Smail 引用同一组文件时，外部工具应成对更新证书与私钥，检查 Nginx 配置后平滑加载；Smail 的运行时证书加载器会检查文件变更，并在新证书无效时保留已有有效证书。
- 使用导入副本或不同路径时，更新 Nginx 的源文件不代表 Smail 已更新，仍需重新导入或同步实际引用的文件。跨服务器同步不能靠共享路径配置完成。
- 手动引用和证书热加载都不等于自动续期。必须另行配置、验证外部续期及部署任务，并分别核验网页和各邮件协议实际使用的证书；不能以页面可访问或文件已替换作为续期验收结论。

## Nginx 配置示例

下面示例将 Nginx 的 15184 本机代理端口替换为实际 Smail HTTP 端口。

```nginx
log_format smail_privacy
    '$time_iso8601 remote=$remote_addr method=$request_method uri=$uri '
    'status=$status bytes=$body_bytes_sent duration=$request_time';

server {
    listen 80;
    listen [::]:80;
    server_name xuans.com;
    access_log /var/log/nginx/smail-access.log smail_privacy;

    location /.well-known/acme-challenge/ {
        # 由反代服务器上的 ACME 客户端写入此目录，不转发到 Smail。
        root /var/www/acme;
        try_files $uri =404;
    }

    location / {
        return 301 https://$host$request_uri;
    }
}

server {
    listen 443 ssl;
    listen [::]:443 ssl;
    server_name xuans.com;
    access_log /var/log/nginx/smail-access.log smail_privacy;

    ssl_certificate /etc/letsencrypt/live/xuans.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/xuans.com/privkey.pem;

    location / {
        proxy_pass http://127.0.0.1:15184;
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto https;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection $connection_upgrade;
    }
}
```

上面的 ACME 目录仅适用于反代服务器使用 HTTP 验证的情况，需与外部 ACME 客户端的网页根目录一致；使用 DNS 验证时无需该路径。

如果 Nginx 配置使用了 `$connection_upgrade`，请在 `http` 配置块中增加：

```nginx
map $http_upgrade $connection_upgrade {
    default upgrade;
    '' close;
}
```

## 直接 HTTPS 模式

不使用 Nginx HTTPS 终止时，保持以下配置或省略 `tlsMode`：

```json
{
  "http": {
    "port": 80,
    "httpsPort": 443,
    "httpsEnabled": true,
    "tlsMode": "direct"
  }
}
```

此模式下由 Smail 监听 HTTPS，HTTP 请求会跳转到 HTTPS。两种模式只能选择一种，不能让 Smail 和 Nginx 同时监听同一个公网 HTTPS 端口。

## 防火墙建议

Nginx 对公网提供 Web 服务时通常开放 `80/tcp` 和 `443/tcp`。Smail 的 `15184/tcp` 仅供本机 Nginx 访问，应保持本机监听或限制为本机来源，不要直接暴露到公网。

邮件协议端口按照实际启用情况开放，例如 `25/tcp`、`465/tcp`、`587/tcp`、`993/tcp`、`995/tcp`；数据库端口不应直接开放公网。IPv6 监听和防火墙规则应同步配置。

## 访问日志保护

`deploy/nginx/smail.conf` 使用专用日志格式，仅保留请求路径及必要的状态信息，不记录查询参数、来源页和认证头，避免微信授权回调、密码恢复链接等敏感参数进入访问日志。不要为 Smail 站点重新启用包含完整请求或来源页的日志格式。

专用日志文件应限制为 `0640` 或更严格权限，并确认日志轮转后的权限不会放宽。不要直接修改共享服务器上其他项目的日志。错误日志和应用日志仍需单独检查，访问日志脱敏不代表所有日志均已完成脱敏。

## 验证清单

- [ ] Smail 日志显示已跳过内部 HTTPS 监听。
- [ ] Smail 只监听配置的内部 HTTP 端口。
- [ ] Nginx 443 代理请求可以打开登录页。
- [ ] 登录请求的 `Origin` 为 `https://xuans.com` 时可以通过校验。
- [ ] 非可信来源伪造 `X-Forwarded-Proto` 不会被信任。
- [ ] 浏览器收到的会话 Cookie 仍带有 `Secure` 和 `HttpOnly` 属性。
- [ ] 证书续期由反代服务器完成，Smail 不再发出 ACME 申请或续期请求。
- [ ] 使用 HTTP 验证时，ACME 路径由反代服务器提供，其他 HTTP 路径跳转到 HTTPS。
- [ ] 本地邮件服务证书保持有效；外部续期后已更新实际引用文件或重新导入，完成 Nginx 加载及邮件协议实际握手核验。
- [ ] Smail 专用访问日志不记录查询参数、来源页和认证头，文件及轮转权限符合要求。
