#!/usr/bin/env bash
set -eu

if [ "$(id -u)" -ne 0 ]; then
  echo "必须使用 root 用户安装 Smail" >&2
  exit 1
fi

root_dir="$(realpath -e -- "${1:-$(dirname "$0")/..}")"
runtime_dir=/opt/smail
case "$(uname -m)" in
  x86_64) arch=amd64 ;;
  aarch64|arm64) arch=arm64 ;;
  *) echo "不支持的系统架构" >&2; exit 1 ;;
esac

# 安装前先检查所有输入和运行状态，不能覆盖仍在运行的程序。
for file in "$root_dir/out/backend/smail_linux_$arch" "$root_dir/out/backend/smail-admin_linux_$arch" \
  "$root_dir/deploy/systemd/smail.service" "$root_dir/deploy/systemd/smail-admin.service"; do
  if [ ! -f "$file" ] || [ -L "$file" ] || [ ! -s "$file" ]; then
    echo "安装输入不存在或不是普通文件：$file" >&2
    exit 1
  fi
done
if systemctl is-active --quiet smail.service || systemctl is-active --quiet smail-admin.service; then
  echo "请先停止 Smail 主服务和管理员重置服务，安装不会自动中断现有业务。" >&2
  exit 1
fi
for process in /proc/[0-9]*/exe; do
  executable="$(readlink "$process" 2>/dev/null || true)"
  case "$executable" in
    "$runtime_dir/smail"|"$runtime_dir/smail (deleted)"|"$runtime_dir/smail-admin"|"$runtime_dir/smail-admin (deleted)")
      echo "部署目录中的程序仍在运行，请确认并停止后再安装。" >&2
      exit 1 ;;
  esac
done
if [ -L /opt ] || [ -L "$runtime_dir" ] || { [ -e "$runtime_dir" ] && [ ! -d "$runtime_dir" ]; }; then
  echo "部署路径不能是链接或普通文件。" >&2
  exit 1
fi
for directory in config data ssl log plugins; do
  target="$runtime_dir/$directory"
  if [ -L "$target" ] || { [ -e "$target" ] && [ ! -d "$target" ]; }; then
    echo "运行目录不能是链接或普通文件：$target" >&2
    exit 1
  fi
done
for target in "$runtime_dir/smail" "$runtime_dir/smail-admin" \
  /etc/systemd/system/smail.service /etc/systemd/system/smail-admin.service \
  /etc/smail/smail.env; do
  if [ -L "$target" ] || { [ -e "$target" ] && [ ! -f "$target" ]; }; then
    echo "程序或服务文件不能是链接或特殊文件：$target" >&2
    exit 1
  fi
done
if [ -L /etc/smail ] || { [ -e /etc/smail ] && [ ! -d /etc/smail ]; }; then
  echo "服务环境目录不能是链接或普通文件：/etc/smail" >&2
  exit 1
fi
if [ -L /run/smail ] || { [ -e /run/smail ] && [ ! -d /run/smail ]; }; then
  echo "本机控制目录不能是链接或普通文件。" >&2
  exit 1
fi
if [ -L /run/smail/admin-control.sock ] || { [ -e /run/smail/admin-control.sock ] && [ ! -S /run/smail/admin-control.sock ]; }; then
  echo "本机控制路径不是安全套接字。" >&2
  exit 1
fi

# 安装备份存放在系统备份目录，不在部署根目录遗留 .bak 文件。
backup_dir="$(mktemp -d /var/backups/smail-deploy-XXXXXXXX)"
for name in smail smail-admin; do
  if [ -f "$runtime_dir/$name" ]; then cp -p -- "$runtime_dir/$name" "$backup_dir/$name"; fi
done
for name in smail.service smail-admin.service; do
  if [ -f "/etc/systemd/system/$name" ]; then cp -p -- "/etc/systemd/system/$name" "$backup_dir/$name"; fi
done
if [ -f /etc/smail/smail.env ]; then cp -p -- /etc/smail/smail.env "$backup_dir/smail.env"; fi

if ! getent group smail >/dev/null; then groupadd --system smail; fi
if ! getent passwd smail >/dev/null; then useradd --system --gid smail --home-dir "$runtime_dir" --shell /usr/sbin/nologin smail; fi
if ! getent group smail-plugin >/dev/null; then groupadd --system smail-plugin; fi
if ! getent passwd smail-plugin >/dev/null; then useradd --system --gid smail-plugin --home-dir /nonexistent --shell /usr/sbin/nologin smail-plugin; fi
install -d -o root -g root -m 0755 "$runtime_dir"
install -d -o root -g root -m 0750 /etc/smail
if [ ! -e /etc/smail/smail.env ]; then
  # 只为新部署设置回收预算；已存在的文件始终保留部署人员的自定义环境配置。
  umask 0077
  printf '%s\n' 'SMAIL_ROOT=/opt/smail' 'GOMEMLIMIT=256MiB' > /etc/smail/smail.env
fi
if ! grep -q '^SMAIL_PLUGIN_UID=' /etc/smail/smail.env; then
  printf 'SMAIL_PLUGIN_UID=%s\n' "$(id -u smail-plugin)" >> /etc/smail/smail.env
fi
if ! grep -q '^SMAIL_PLUGIN_GID=' /etc/smail/smail.env; then
  printf 'SMAIL_PLUGIN_GID=%s\n' "$(id -g smail-plugin)" >> /etc/smail/smail.env
fi
chown root:root /etc/smail/smail.env
chmod 0600 /etc/smail/smail.env
for directory in config data ssl log plugins; do
  target="$runtime_dir/$directory"
  # 内置插件在临时沙箱中展开，只兼容已存在的历史插件目录。
  if [ "$directory" = plugins ] && [ ! -d "$target" ]; then continue; fi
  install -d -o smail -g smail -m 0700 "$target"
  # 不跟随链接，也不越过挂载点；仅为本部署的存量运行数据调整属主。
  find -P "$target" -xdev \( -type d -o -type f \) -exec chown --no-dereference smail:smail {} +
done
install -d -o smail -g smail -m 0750 /run/smail
if [ -S /run/smail/admin-control.sock ]; then
  # 从旧版 root 主服务迁移时保留残留套接字，由主程序验证失活后自行处理。
  chown --no-dereference smail:smail /run/smail/admin-control.sock
fi
install -d -o root -g root -m 0755 /run/systemd/ask-password
install -o root -g root -m 0755 "$root_dir/out/backend/smail_linux_$arch" "$runtime_dir/smail"
install -o root -g root -m 0700 "$root_dir/out/backend/smail-admin_linux_$arch" "$runtime_dir/smail-admin"
install -o root -g root -m 0644 "$root_dir/deploy/systemd/smail.service" /etc/systemd/system/smail.service
install -o root -g root -m 0644 "$root_dir/deploy/systemd/smail-admin.service" /etc/systemd/system/smail-admin.service
systemctl daemon-reload
systemctl enable smail
# 清理旧版本可能留下的开机启动关系。smail-admin 单元没有 [Install]，不能再次 enable。
systemctl disable smail-admin >/dev/null 2>&1 || true
echo "Smail 已安装到 $runtime_dir，旧程序和服务文件备份在 $backup_dir。"
echo "配置、数据库和证书未覆盖；请使用 systemctl start smail 启动并检查运行状态。"
echo "smail-admin 仅允许按需执行 start/stop，不支持开机自启动。"
echo "主服务默认内存高水位 384 MiB、硬上限 512 MiB；不是容量承诺，请先核对同机资源预算。"
echo "旧环境文件不会自动补写 GOMEMLIMIT；启动后请按《生产资源部署说明》执行只读资源预检。"
