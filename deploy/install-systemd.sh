#!/usr/bin/env bash
set -eu

usage() {
  echo "用法：$0 [发布目录]"
  echo "每次部署都是全新部署，必须输入确认词后才会清理 Smail 专属运行数据。"
}

fresh=true
root_argument=""
while [ "$#" -gt 0 ]; do
  case "$1" in
    --fresh)
      # 保留旧参数兼容性；部署始终按全新部署处理。
      ;;
    --help|-h)
      usage
      exit 0
      ;;
    -* )
      echo "未知参数：$1" >&2
      usage >&2
      exit 1
      ;;
    *)
      if [ -n "$root_argument" ]; then
        echo "发布目录只能指定一次。" >&2
        exit 1
      fi
      root_argument="$1"
      ;;
  esac
  shift
done

if [ "$(id -u)" -ne 0 ]; then
  echo "必须使用 root 用户安装 Smail" >&2
  exit 1
fi

root_dir="$(realpath -e -- "${root_argument:-$(dirname "$0")/..}")"
is_packaged=false
if [ -f "$root_dir/smail" ] && [ -f "$root_dir/smail-admin" ] && [ -f "$root_dir/smail-config" ]; then
  is_packaged=true
fi
runtime_dir="${root_dir}"
fresh_confirmation="确认全新部署 Smail"
printf '这是不可逆的 Smail 专属数据清理操作。请输入“%s”继续： ' "$fresh_confirmation"
if ! IFS= read -r confirmation || [ "$confirmation" != "$fresh_confirmation" ]; then
  echo "未获得有效确认，已取消全新部署。" >&2
  exit 1
fi
case "$(uname -m)" in
  x86_64) arch=amd64 ;;
  aarch64|arm64) arch=arm64 ;;
  *) echo "不支持的系统架构" >&2; exit 1 ;;
esac

# 安装前先检查所有输入和运行状态，不能覆盖仍在运行的程序。
if [ "$is_packaged" = true ]; then
  backend_smail="$root_dir/smail"
  backend_smail_admin="$root_dir/smail-admin"
  backend_smail_config="$root_dir/smail-config"
else
  backend_smail="$root_dir/out/backend/smail_linux_$arch"
  backend_smail_admin="$root_dir/out/backend/smail-admin_linux_$arch"
  backend_smail_config="$root_dir/out/backend/smail-config_linux_$arch"
fi

for file in "$backend_smail" "$backend_smail_admin" \
  "$backend_smail_config" \
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
if [ -L "$runtime_dir" ] || { [ -e "$runtime_dir" ] && [ ! -d "$runtime_dir" ]; }; then
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

# 仅保留程序和服务文件的短期备份，不保留配置、数据库、证书、日志、插件或其他运行数据。
for legacy_backup in /var/backups/smail-deploy-*; do
  if [ ! -e "$legacy_backup" ]; then continue; fi
  if [ -L "$legacy_backup" ] || [ ! -d "$legacy_backup" ]; then
    echo "历史 Smail 备份不是安全目录，拒绝清理：$legacy_backup" >&2
    exit 1
  fi
  rm -rf -- "$legacy_backup"
done
if [ "$fresh" = true ]; then
  if [ -d "$runtime_dir" ] && [ -n "$(find "$runtime_dir" -mindepth 1 -maxdepth 1 -type l -print -quit)" ]; then
    echo "全新部署已停止：/opt/smail 顶层存在符号链接，拒绝清理。" >&2
    exit 1
  fi

  if [ -d "$runtime_dir" ]; then
    if [ "$is_packaged" = true ]; then
      for directory in config data ssl log; do
        if [ -d "$runtime_dir/$directory" ]; then
          find "$runtime_dir/$directory" -mindepth 1 -maxdepth 1 -exec rm -rf -- {} +
        fi
      done
    else
      find "$runtime_dir" -mindepth 1 -maxdepth 1 -exec rm -rf -- {} +
    fi
  fi
  if [ -d /etc/smail ]; then
    find /etc/smail -mindepth 1 -maxdepth 1 -exec rm -rf -- {} +
  fi
  if [ -d /etc/systemd/system/smail.service.d ]; then
    rm -rf -- /etc/systemd/system/smail.service.d
  fi
  if [ -d /run/smail ]; then
    find /run/smail -mindepth 1 -maxdepth 1 -exec rm -rf -- {} +
  fi
  systemctl disable smail smail-admin >/dev/null 2>&1 || true
  systemctl daemon-reload
fi

if ! getent group smail >/dev/null; then groupadd --system smail; fi
if ! getent passwd smail >/dev/null; then useradd --system --gid smail --home-dir "$runtime_dir" --shell /usr/sbin/nologin smail; fi
install -d -o smail -g smail -m 0755 "$runtime_dir"
chown smail:smail "$runtime_dir"
install -d -o root -g root -m 0750 /etc/smail
if [ ! -e /etc/smail/smail.env ]; then
  # 每次部署都重新创建服务环境文件，不继承旧的凭据或代理配置。
  umask 0077
  printf '%s\n' "SMAIL_ROOT=$runtime_dir" 'GOMEMLIMIT=256MiB' > /etc/smail/smail.env
fi
if ! grep -q '^SMAIL_PLUGIN_UID=' /etc/smail/smail.env; then
  printf 'SMAIL_PLUGIN_UID=%s\n' "$(id -u smail)" >> /etc/smail/smail.env
fi
if ! grep -q '^SMAIL_PLUGIN_GID=' /etc/smail/smail.env; then
  printf 'SMAIL_PLUGIN_GID=%s\n' "$(id -g smail)" >> /etc/smail/smail.env
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
if [ "$backend_smail" != "$runtime_dir/smail" ]; then
  install -o root -g root -m 0755 "$backend_smail" "$runtime_dir/smail"
else
  chown root:root "$runtime_dir/smail"
  chmod 0755 "$runtime_dir/smail"
fi

if [ "$backend_smail_admin" != "$runtime_dir/smail-admin" ]; then
  install -o root -g root -m 0700 "$backend_smail_admin" "$runtime_dir/smail-admin"
else
  chown root:root "$runtime_dir/smail-admin"
  chmod 0700 "$runtime_dir/smail-admin"
fi

if [ "$backend_smail_config" != "$runtime_dir/smail-config" ]; then
  install -o root -g root -m 0700 "$backend_smail_config" "$runtime_dir/smail-config"
else
  chown root:root "$runtime_dir/smail-config"
  chmod 0700 "$runtime_dir/smail-config"
fi
sed -e "s|WorkingDirectory=/opt/smail|WorkingDirectory=$runtime_dir|g" \
    -e "s|ExecStart=/opt/smail/smail|ExecStart=$runtime_dir/smail|g" \
    -e "s|/opt/smail/config|$runtime_dir/config|g" \
    -e "s|/opt/smail/data|$runtime_dir/data|g" \
    -e "s|/opt/smail/ssl|$runtime_dir/ssl|g" \
    -e "s|/opt/smail/log|$runtime_dir/log|g" \
    -e "s|/opt/smail/plugins|$runtime_dir/plugins|g" \
    "$root_dir/deploy/systemd/smail.service" > /etc/systemd/system/smail.service
chmod 0644 /etc/systemd/system/smail.service
chown root:root /etc/systemd/system/smail.service

sed -e "s|WorkingDirectory=/opt/smail|WorkingDirectory=$runtime_dir|g" \
    -e "s|ExecStart=/opt/smail/smail-admin|ExecStart=$runtime_dir/smail-admin|g" \
    "$root_dir/deploy/systemd/smail-admin.service" > /etc/systemd/system/smail-admin.service
chmod 0644 /etc/systemd/system/smail-admin.service
chown root:root /etc/systemd/system/smail-admin.service
systemctl daemon-reload
systemctl enable smail
systemctl disable smail-admin >/dev/null 2>&1 || true

open_firewall_ports() {
  local ports="80 443 25 465 587 110 995 143 993"
  echo "=== 正在检查并自动开放网络防火墙端口 ==="
  local opened=false

  # 1. 检查 ufw (Ubuntu / Debian 常用)
  if command -v ufw >/dev/null 2>&1; then
    if ufw status 2>/dev/null | grep -q "Status: active"; then
      echo "检测到 ufw 防火墙处于开启状态，自动添加放行规则..."
      for p in $ports; do
        ufw allow "${p}/tcp" >/dev/null 2>&1 || true
      done
      opened=true
      echo "ufw 端口放行完成 (80, 443, 25, 465, 587, 110, 995, 143, 993)。"
    else
      echo "检测到 ufw 未激活，跳过 ufw 规则添加。"
    fi
  fi

  # 2. 检查 firewalld (CentOS / RHEL / Rocky / Fedora 常用)
  if command -v firewall-cmd >/dev/null 2>&1; then
    if systemctl is-active --quiet firewalld; then
      echo "检测到 firewalld 处于运行状态，自动添加放行规则..."
      for p in $ports; do
        firewall-cmd --permanent --add-port="${p}/tcp" >/dev/null 2>&1 || true
      done
      firewall-cmd --reload >/dev/null 2>&1 || true
      opened=true
      echo "firewalld 端口放行并重载完成 (80, 443, 25, 465, 587, 110, 995, 143, 993)。"
    else
      echo "检测到 firewalld 未运行，跳过 firewalld 规则添加。"
    fi
  fi

  # 3. 检查 iptables (若前两者未激活或不存在，但 iptables 命令存在)
  if [ "$opened" = false ] && command -v iptables >/dev/null 2>&1; then
    for p in $ports; do
      if ! iptables -C INPUT -p tcp --dport "$p" -j ACCEPT >/dev/null 2>&1; then
        iptables -I INPUT -p tcp --dport "$p" -j ACCEPT >/dev/null 2>&1 || true
      fi
    done
    echo "iptables INPUT 规则已更新放行 (80, 443, 25, 465, 587, 110, 995, 143, 993)。"
  fi
}

open_firewall_ports

echo "=== 正在启动 Smail 系统服务 ==="
systemctl restart smail

echo "=== Smail 服务状态 ==="
systemctl status smail --no-pager || true

echo "=========================================================="
echo " Smail 安装与系统服务注册完成！"
echo " 安装路径: $runtime_dir"
echo " 运行用户: smail"
echo " 开机自启: 已启用 (systemctl enable smail)"
echo " 服务状态: 已启动 (systemctl status smail)"
echo "=========================================================="

# 实时追踪服务日志，直到检测到初始化完成（出现“Setup End!”或“初始化完成”）后自动退出
echo ""
echo "=== 正在等待并监控初始化流程 (按 Ctrl+C 可提前退出监控) ==="
echo "提示：请在浏览器打开初始化页面完成系统初始化配置..."

# 使用 journalctl 持续监听 smail 服务日志
journalctl -u smail -f -n 20 --no-pager | while IFS= read -r line; do
  printf '%s\n' "$line"
  case "$line" in
    *"Setup End!"*)
      printf '\n%s\n' "=========================================================="
      printf '%s\n' " 检测到 Smail 初始化已顺利完成 (Setup End!)！监控自动退出。"
      printf '%s\n' "=========================================================="
      break
      ;;
  esac
done
