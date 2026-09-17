# HTTP 无停机续期验收记录

## 范围与结论

时间为 2026 年 9 月 18 日 02:15 至 02:27，Asia/Shanghai。测试只在 `47.108.78.12`、主机名 `ikun2` 执行。本机仅编辑、Git、传输和静态检查。修改前基线提交及推送为 `cfa41e1`。

- [✓] 五个域名均解析到 47 服务器；现有 Nginx 模板已经为验证路径提供独立 Webroot。
- [✓] 候选增加显式 HTTP 模式，不停止 Nginx，不绑定验证端口，不从 DNS 自动降级。
- [✓] 41 项远程隔离回归通过，含原有证书校验、真实隔离 Nginx 平滑重载及失败回退，以及 HTTP 目录、掩码、安全失败行为。
- [✓] 全新测试账号的真实公网挑战失败期间，两站点 138 次 HTTPS 探测通过，正式文件与四服务进程不变，验证文件由 ACME 客户端清理，测试单元已停止。
- [!] 五个域名的真实 HTTP-01 挑战均被 CA 判为 403；HTTP 模式不能启用，未取得或部署测试证书。
- [!] 线上仍是旧续期脚本和定时任务；专用 DNS 授权、正式新证书发布、外部告警仍未验收，不满足生产放行。

## 候选实现

`deploy/nginx/renew_certificate.py` 默认仍为 DNS 模式。设置 `SMAIL_ACME_CHALLENGE=http` 时，使用 `--http --http.webroot /var/lib/smail-acme`，不启用 lego 内置 HTTP 服务器。未知或空验证方式拒绝执行；DNS 缺少服务商时不自动切换方式。

HTTP 模式要求 ACME 账号目录由 root 独占且没有符号链接；公开验证目录及两层子目录必须为 root 所有、其他用户不可写且可读取、遍历。lego 4.34.0 的 Webroot 实现以 0644 创建验证文件，服务级 `UMask=0077` 会使它变为 0600。因此仅 HTTP 子进程设置 `umask=0022`，父进程和 DNS 子进程保持 0077。账号与证书目录仍为 root 独占；公开目录不保存账号、证书或私钥。

候选 systemd 服务增加可选 `/etc/smail/acme-renew.env`。仓库 `acme-renew.env.example` 仅为示例，不代表已经配置或允许上线。实际候选未安装到 `/usr/local/libexec/smail-acme`，正式 service 和 timer 均未替换。

## 隔离回归

目录为 `/var/tmp/smail-review-http-renew-20260918`。

- `tests-022115.txt`：39 项通过，2.594 秒，峰值 23.6 MiB，交换空间为零。
- `tests-022511.txt`：增加掩码及私有账号目录回归后，41 项通过，零跳过；单元 `smail-webroot-tests-20260918-022511` 运行 2.217 秒，峰值 23.9 MiB，交换空间为零。

单元使用独立网络、私有临时目录、严格只读系统，线上应用、正式 ACME 状态、管理 socket、Nginx 配置和公开 Webroot 均不可访问。唯一可写业务目录是本轮测试目录，内存上限 128 MiB。测试以 root 执行；真实 Nginx 仅运行在隔离网络，不影响主机服务。

## 全新公网挑战

工具为 `test/deployment/webroot_renewal_drill.py`。显式参数 `--confirm-staging-challenge`、目标主机、Nginx 模板摘要、五域名解析和可用内存检查通过后，创建空的 root 独占目录，不复制任何既有 ACME 账号或授权缓存。

- 证据目录：`/var/tmp/smail-review-http-challenge-20260918-022531`。
- 独立账号目录：`/var/lib/smail-acme-http-review-20260918-022531`，权限 0700，仅保存在服务器。
- ACME 单元：`smail-http-challenge-20260918-022531`，运行 34.751 秒，峰值 10.6 MiB，交换空间为零，退出码 1。
- `acme.log` 记录新账号密钥生成、五份新授权、五次 HTTP-01 求解及 403 失败；不是缓存授权续期。
- 单元无法访问线上应用、管理 socket、Nginx 配置及正式 ACME 状态；仅独立账号目录和公开验证子目录可写。未调用正式部署钩子。

`result.json` 为 `failed`，但 `servicesUnchanged`、`protectedUnchanged`、`challengeFilesRestored`、`probesPassed`、`testUnitStopped` 均为真。两个站点累计 138 次可信 HTTPS 探测全部通过。Nginx 主进程 578671、Smail 574584、Scert 589832、MySQL 589785 及 InvocationID 均保持不变。

## 403 的证据边界

02:27 只读核查显示，服务器自身通过回环或公网 IP 访问不存在的验证文件均返回 Nginx 的 404，没有错误跳转。Nginx 访问日志在 02:25:38、43、48、54、59 记录来自 CA 验证节点的五个实际挑战请求，状态均为 200，其中两次发送 87 字节，三次发送字节数为零；错误日志没有对应文件权限错误。

与此同时，CA 报告五个域名均收到 403。这证明本站成功响应不足以代表 CA 验证成功；尚未确定是某条公网路径、其他验证节点还是中间设备造成差异。没有据此宣称云厂商或本站某条规则是已确认根因，也未通过重复申请掩盖失败。

## 后续门槛

1. 优先取得 Smail 专用最小权限 DNS 授权，在远程受保护文件配置，不借用其他项目凭据，不在聊天或仓库保存秘密。
2. 如继续 HTTP 方案，先定位公网 403 差异，再用全新测试账号确认五个域名全部通过；当前不能直接部署。
3. 验证正式签发、同密钥发布、平滑重载、七个 TLS 入口及故障回退，再同步安装续期脚本、发布脚本和 service，避免新旧锁协议混用。
4. 接入真实失败及到期告警；业务浏览器、管理员恢复、容量及灾难恢复等原发布门槛不因此缩减。
