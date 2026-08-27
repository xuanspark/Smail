#!/usr/bin/env bash
set -eu

if [ "$(id -u)" -ne 0 ]; then
  echo "必须使用 root 用户安装 Smail" >&2
  exit 1
fi

root_dir="${1:-$(cd "$(dirname "$0")/.." && pwd)}"
install -d -m 0750 /var/lib/smail /run/smail /usr/local/lib/smail
if ! getent group smail >/dev/null; then groupadd --system smail; fi
if ! getent passwd smail >/dev/null; then useradd --system --gid smail --home-dir /var/lib/smail --shell /usr/sbin/nologin smail; fi
chown smail:smail /var/lib/smail
install -o root -g root -m 0755 "$root_dir/server/smail" /usr/local/lib/smail/smail
install -o root -g root -m 0750 "$root_dir/server/smail-admin" /usr/local/lib/smail/smail-admin
install -o root -g root -m 0644 "$root_dir/deploy/systemd/smail.service" /etc/systemd/system/smail.service
install -o root -g root -m 0644 "$root_dir/deploy/systemd/smail-admin.service" /etc/systemd/system/smail-admin.service
chown smail:smail /run/smail
chmod 0750 /run/smail
systemctl daemon-reload
systemctl enable smail
# 清理旧版本可能留下的开机启动关系。smail-admin 单元没有 [Install]，不能再次 enable。
systemctl disable smail-admin >/dev/null 2>&1 || true
echo "Smail 安装完成；smail-admin 仅允许按需执行 start/stop，不支持开机自启动。"
