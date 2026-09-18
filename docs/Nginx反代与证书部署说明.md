# Nginx 反代与证书部署说明

> 当前状态（2026-09-18）：按用户要求，服务器外部 Python/lego 续期程序及 timer 已移除。项目自身托管使用内置续期；手动与代理托管不由 Smail 自动续期。下文的外部脚本与定时任务是历史记录，不再作为部署步骤。最新实现和验收边界见《证书托管续期策略验收》。

## 最新整改状态

远程主机 2026-09-18 00:22 已将 Nginx 证书路径迁移为 `/opt/smail/ssl/custom.crt`、`custom.key`，不再直接使用签发目录。只对 Smail 站点执行路径迁移和平滑重载，正式证书、应用程序、其他站点配置未改动。58 次双站 HTTPS 探测和预先建立的下载连接通过，七个 TLS/STARTTLS 入口再次通过。

仓库中的续期和部署脚本已改为 DNS 验证、校验快照、平滑重载、实际指纹检查及失败回退，28 项远程隔离回归通过。由于缺少专用 DNS 授权和真实挑战证据，线上续期脚本及 systemd 单元尚未替换，旧自动任务仍然存在到期停机风险。不得把仓库候选代码、未到期检查或路径迁移当作无停机续期已经启用。

实时配置确认 Scert 已使用 `web.apmmk.cn:443`，不再监听旧记录中的 8003/8004。本轮没有修改其配置。完整证据、失败过程和启用门槛见《无停机续期整改记录》；以下签发和维护窗口章节保留原部署过程。

## 已部署拓扑

2026-09-17 在 47.108.78.12 部署，本机不运行应用测试。

| 入口 | 处理方式 |
| --- | --- |
| 公网 HTTP 80 | Nginx 处理域名校验与 HTTPS 跳转 |
| 公网 HTTPS 443 | Nginx 终止 TLS，反代至 `https://127.0.0.1:8002` |
| Smail HTTP 8001 | 保留应用 HTTP 监听，仅内网诊断，不作为业务反代上游 |
| Smail HTTPS 8002 | 业务上游，Nginx 校验系统根链和 `mail.solit.cn` 主机名 |
| SMTP 25/587、465、IMAP 993、POP3 995 | Smail 直接提供邮件协议及可信 TLS |

应用当前监听通配地址，已通过现有 UFW 的 IPv4/IPv6 优先规则拒绝外部连接 8001/8002，回环访问正常。不要对公网开放上游端口。未从另一台主机扫描防火墙，因此不把规则检查称为独立外网阻断验收。

应用采用 `direct` TLS 模式，不能改成 `proxy` 模式后仍期待 8002 监听。Nginx 到上游继续使用 TLS，不关闭 `proxy_ssl_verify`。外部 HTTP 由 Nginx 直接跳转，避免应用的内部跳转地址泄露 8002。

应用配置 `http.port=8001`、`http.httpsPort=8002`、`http.httpsEnabled=true`，配置代次由 1 更新为 2，所有分区代次及摘要同步更新。安全密钥来源显式设置为 `https://mail.solit.cn`，不继承上游端口。

## 配置与证书

- 版本库模板：`deploy/nginx/nginx.conf`、`deploy/nginx/smail.conf`。模板用于本服务器，不应覆盖已有其他站点的 Nginx 主配置。
- 实际配置：`/etc/nginx/nginx.conf`、`/etc/nginx/conf.d/smail.conf`。
- 签发者：Let's Encrypt YR1，RSA 2048。
- SAN：`solit.cn`、`mail.solit.cn`、`smtp.solit.cn`、`imap.solit.cn`、`pop.solit.cn`。
- 有效期：2026-09-17 13:54:40 UTC 至 2026-12-16 13:54:39 UTC。
- 叶证书 SHA256：`608fb55e3b8add3426a63f672debd86a58389a1a78d0961b9603b2e1abecae43`。
- ACME 状态目录：`/var/lib/smail-acme-tls`，0700，仅 root 使用。
- Nginx 证书：现为 `/opt/smail/ssl/custom.crt`、`custom.key`；初次部署曾直接使用上述签发目录，最新整改已分离。
- Smail 证书：`/opt/smail/ssl/custom.crt` 和 `custom.key`，属主 smail，0600。新握手热加载，不为换证重启邮件服务。

HTTP-01 测试失败，CA 报告部分节点收到 403 或连接重置，服务器 Nginx 仅记录部分节点的 200。外部路径原因尚未定位，不能断言具体运营商策略。改用 TLS-ALPN-01 后，五个域名的测试和正式签发均成功，没有使用 DNS 密钥。Webroot 在原理上可以避免停机，但本项目尚未证明公网挑战路径可用，不能将其直接视为已验收的替代方案。

lego 固定为项目同版本 v4.34.0，官方发布包 SHA256 为 `c681c09d3f2ae37a8d0fe744b55a7d00321832ab603d5bf1b484d66d41b06cd2`，下载后核对摘要才执行。ACME 未配置联系邮箱，不依赖邮件通知到期。

## 续期运维

本节描述当前仍在运行的旧续期任务，不是仓库中待启用的 DNS 候选。两个新脚本必须成套部署，不能将带互斥锁的新部署脚本单独混入持锁后再启动子进程的旧续期实现。

脚本安装在 `/usr/local/libexec/smail-acme/`。`smail-certificate-renew.timer` 每日 04:00 加 0 至 30 分钟错峰运行；未到期时校验证书并确认运行文件已同步，不暂停 Web。剩余不足 30 天时执行续期，复用密钥，避免证书与私钥切换间隙。

续期会暂停 Nginx 使用公网 443。执行上限 150 秒，服务总超时 240 秒，正常异常路径使用 `finally` 恢复，systemd `ExecStopPost` 额外覆盖任务终止后的恢复。邮件服务不随 Nginx 停止。此方案存在短暂 Web 维护窗口，不是无中断续期。

2026-09-17 23:30 复核发现，同一个 Nginx 已加载 `scert-apmmk.conf`，同时承载 Scert 的 8003/8004 入口。当前续期脚本执行的是整个 `nginx.service` 的停止，因此到期续期也会中断这些入口。六项模拟回归通过不能消除该跨项目影响；在明确共用服务维护窗口或完成不中断其他站点的挑战方案前，不得把自动续期列为生产验收通过。本轮没有停止或重载 Nginx，没有触发真实到期续期，也没有修改 Scert 配置。

23:48 至 23:51 更新：用户随后授权测试期间暂时停止其他服务。已在 128 MiB 限额内完成真实测试环境续期，以及进程被 systemd 超时终止后的自动恢复演练；两轮分别约 9.27 秒和 20 秒后恢复 Nginx，Smail 与 Scert HTTPS 入口均已复核恢复，正式证书未更换。CA 复用已有授权，没有重新验证公网挑战；正式换证及外部告警仍未验收。此授权不自动等同于长期接受定时续期的共用维护窗口，详情见《续期维护窗口演练记录》。

续期服务预算为 MemoryHigh 96 MiB、MemoryMax 128 MiB、SwapMax 0、CPUQuota 100%、TasksMax 64。已演练未到期真实任务、模拟失败回归、真实测试环境续期和 systemd 超时恢复；尚未在此预算内完成全新挑战及正式到期换证完整链路，不能将已通过的分项扩大为全部续期验收完成。

同步脚本先检查系统信任链、五个域名、至少七天有效期与密钥匹配，再保存回退副本并更新运行文件。失败返回非零，不静默忽略。安装的 Certbot 保留，但其自动定时器已停用，避免双重续期管理。

```bash
systemctl list-timers smail-certificate-renew.timer
systemctl status smail-certificate-renew.service
journalctl -u smail-certificate-renew.service
systemctl start smail-certificate-renew.service
python3 /var/tmp/smail-review-nginx-20260917/verify_nginx_tls.py
```

仍需接入续期任务失败及证书剩余天数的外部告警。不要通过关闭证书校验掩盖过期或续期失败。

## 备份与验收边界

- 部署前停服一致性备份：`/var/backups/smail-nginx-20260917T144222Z/smail-state.tar.gz`，0600；摘要 `29bd3deb4b498ee873d83cd5986cfd37b2c2c3cf08347a6b913e0c9ea8b41965`。
- 同目录保留原应用配置、UFW、systemd、Nginx 包默认配置及受保护的 `acme-production.tar.gz`。不将备份或密钥提交仓库。
- 旧证书回退副本：`/var/backups/smail-certificate-20260917T145428-4ffa854d`。旧证书为自签名，只供故障回退，不可用于可信 TLS 放行。
- 本轮为配置、证书和无业务写入的传输层验收，未清库、未重置密码、未替换应用双程序。程序摘要仍与 `review-2263779` 一致。

已验证 443、8002、465、993、995 直接 TLS 及内部 25/587 STARTTLS，全部使用系统根链与主机名校验。未使用 `-k` 或关闭证书校验。未知 Host/SNI 拒绝、公网 80 跳转、首页和入口静态资源检查通过。

待整改：应用登录及验证限流仍以 TCP 对端地址识别来源，经 Nginx 后会共享回环地址；恢复页面的默认外部 URL/端口仍可能继承 8002，需要独立配置和回归。当前没有启用恢复专用域名，不能把该路径列为验收通过。真实浏览器、完整发信、管理员恢复撤销、容量、凭据轮换和异机恢复均不因本轮证书签发而自动通过。
