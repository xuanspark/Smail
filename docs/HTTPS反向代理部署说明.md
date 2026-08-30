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

- `tlsMode: proxy`：启用反向代理 HTTPS 终止模式。
- `httpsEnabled: false`：Smail 不启动内部 HTTPS。
- `httpsPort: 0`：避免遗留 HTTPS 端口配置造成误解或端口冲突。
- `trustedProxyCidrs`：只允许这些来源提交 `X-Forwarded-Proto`。Nginx 与 Smail 同机时，默认使用回环地址即可。
- 不要把 `0.0.0.0/0` 或 `::/0` 配置为可信代理网段。

修改配置后需要重启 Smail，使监听模式生效。

## Nginx 配置示例

下面示例将 Nginx 的 15184 本机代理端口替换为实际 Smail HTTP 端口。

```nginx
server {
    listen 80;
    listen [::]:80;
    server_name xuans.com;

    location /.well-known/acme-challenge/ {
        proxy_pass http://127.0.0.1:15184;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto http;
    }

    location / {
        return 301 https://$host$request_uri;
    }
}

server {
    listen 443 ssl;
    listen [::]:443 ssl;
    server_name xuans.com;

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

## 验证清单

- [ ] Smail 日志显示已跳过内部 HTTPS 监听。
- [ ] Smail 只监听配置的内部 HTTP 端口。
- [ ] Nginx 443 代理请求可以打开登录页。
- [ ] 登录请求的 `Origin` 为 `https://xuans.com` 时可以通过校验。
- [ ] 非可信来源伪造 `X-Forwarded-Proto` 不会被信任。
- [ ] 浏览器收到的会话 Cookie 仍带有 `Secure` 和 `HttpOnly` 属性。
- [ ] 80 端口的 ACME challenge 可以正常转发，其他 HTTP 路径跳转到 HTTPS。
