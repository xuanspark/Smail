#!/usr/bin/env bash
set -eu

# Smail 生产环境版本平滑升级脚本
# 规则说明：
# 1. 严格以现有生产环境的实际安装路径作为更新路径，严禁以升级包解压所在路径作为运行路径。
# 2. 仅更新程序文件与系统服务，严密保留现有数据库、配置文件、证书与域名，免去重新初始化流程。

usage() {
  echo "用法：$0 [现有安装目录]"
  echo "默认将自动从 /etc/systemd/system/smail.service 中解析实际安装路径（如 /smail 或 /opt/smail）。"
  echo "如果服务未安装或需要手动指定，可在参数中显式传入安装目录。"
}

if [ "${1:-}" = "-h" ] || [ "${1:-}" = "--help" ]; then
  usage
  exit 0
fi

if [ "$(id -u)" -ne 0 ]; then
  echo "错误：必须使用 root 用户执行升级！" >&2
  exit 1
fi

script_dir="$(cd "$(dirname "$0")" && pwd)"
package_root="$(realpath -e -- "$script_dir/..")"

# 1. 自动探查并确定系统的实际安装路径（严禁使用当前升级包的解压路径）
target_dir="${1:-}"

if [ -z "$target_dir" ]; then
  # 优先从 smail.service 中读取 WorkingDirectory
  if [ -f /etc/systemd/system/smail.service ]; then
    service_workdir="$(grep -E '^[[:space:]]*WorkingDirectory=' /etc/systemd/system/smail.service | head -n1 | cut -d'=' -f2- | tr -d ' ' || true)"
    if [ -n "$service_workdir" ] && [ -d "$service_workdir" ]; then
      target_dir="$service_workdir"
    fi
  fi
fi

if [ -z "$target_dir" ]; then
  # 其次检查常见的默认安装目录
  for candidate in /smail /opt/smail; do
    if [ -d "$candidate" ] && [ -f "$candidate/smail" ] && [ "$candidate" != "$package_root" ]; then
      target_dir="$candidate"
      break
    fi
  done
fi

if [ -z "$target_dir" ]; then
  echo "错误：未能在系统服务中检测到 Smail 的现有安装路径！" >&2
  echo "请手动指定目标安装目录，例如：$0 /smail" >&2
  exit 1
fi

target_dir="$(realpath -m -- "$target_dir")"

if [ ! -d "$target_dir" ]; then
  echo "错误：目标安装目录不存在: $target_dir" >&2
  exit 1
fi

if [ ! -f "$target_dir/smail" ]; then
  echo "警告：在目标目录 $target_dir 下未检测到旧的 smail 主程序，请确认是否为正确的安装目录！"
fi

echo "=========================================================="
echo "               Smail 服务平滑更新                        "
echo " 升级包解压源目录 : $package_root"
echo " 目标系统安装目录 : $target_dir"
echo "=========================================================="

case "$(uname -m)" in
  x86_64) arch=amd64 ;;
  aarch64|arm64) arch=arm64 ;;
  *) echo "不支持的系统架构: $(uname -m)" >&2; exit 1 ;;
esac

# 2. 定位新版本程序二进制文件
if [ -f "$package_root/smail" ] && [ -f "$package_root/smail-admin" ] && [ -f "$package_root/smail-config" ]; then
  new_smail="$package_root/smail"
  new_smail_admin="$package_root/smail-admin"
  new_smail_config="$package_root/smail-config"
elif [ -f "$package_root/out/backend/smail_linux_$arch" ]; then
  new_smail="$package_root/out/backend/smail_linux_$arch"
  new_smail_admin="$package_root/out/backend/smail-admin_linux_$arch"
  new_smail_config="$package_root/out/backend/smail-config_linux_$arch"
else
  echo "错误：未能找到升级包中的二进制程序文件！" >&2
  exit 1
fi

for bin in "$new_smail" "$new_smail_admin" "$new_smail_config"; do
  if [ ! -f "$bin" ] || [ ! -s "$bin" ]; then
    echo "错误：待升级程序文件无效或为空: $bin" >&2
    exit 1
  fi
done

# 3. 停止运行中的服务以解除文件占用
echo ">>> 正在停止运行中的 Smail 服务..."
systemctl stop smail.service 2>/dev/null || true
systemctl stop smail-admin.service 2>/dev/null || true

# 4. 确保运行用户与组存在
if ! getent group smail >/dev/null; then
  groupadd --system smail
fi
if ! getent passwd smail >/dev/null; then
  useradd --system --gid smail --home-dir "$target_dir" --shell /usr/sbin/nologin smail
fi

# 5. 替换程序二进制文件至安装目录（保持安装目录独立，防止覆盖自身报错）
echo ">>> 正在更新主程序及管理工具至: $target_dir"
if [ "$new_smail" != "$target_dir/smail" ]; then
  cp -f "$new_smail" "$target_dir/smail"
fi
chown root:root "$target_dir/smail"
chmod 0755 "$target_dir/smail"

if [ "$new_smail_admin" != "$target_dir/smail-admin" ]; then
  cp -f "$new_smail_admin" "$target_dir/smail-admin"
fi
chown root:root "$target_dir/smail-admin"
chmod 0700 "$target_dir/smail-admin"

if [ "$new_smail_config" != "$target_dir/smail-config" ]; then
  cp -f "$new_smail_config" "$target_dir/smail-config"
fi
chown root:root "$target_dir/smail-config"
chmod 0700 "$target_dir/smail-config"

# 同时更新安装目录下的 deploy 脚本供日后维护
if [ "$package_root" != "$target_dir" ] && [ -d "$package_root/deploy" ]; then
  mkdir -p "$target_dir/deploy"
  cp -rf "$package_root/deploy"/* "$target_dir/deploy/" 2>/dev/null || true
  chmod -R 0755 "$target_dir/deploy" 2>/dev/null || true
fi

# 6. 更新 systemd 服务单元（配置安装路径为工作目录）
systemd_src="$package_root/deploy/systemd"
if [ ! -d "$systemd_src" ] && [ -d "$target_dir/deploy/systemd" ]; then
  systemd_src="$target_dir/deploy/systemd"
fi

if [ -f "$systemd_src/smail.service" ]; then
  sed -e "s|WorkingDirectory=[^ ]*|WorkingDirectory=$target_dir|g" \
      -e "s|ExecStart=[^ ]*/smail|ExecStart=$target_dir/smail|g" \
      -e "s|/opt/smail/config|$target_dir/config|g" \
      -e "s|/opt/smail/data|$target_dir/data|g" \
      -e "s|/opt/smail/ssl|$target_dir/ssl|g" \
      -e "s|/opt/smail/log|$target_dir/log|g" \
      -e "s|/opt/smail/plugins|$target_dir/plugins|g" \
      "$systemd_src/smail.service" > /etc/systemd/system/smail.service
  chmod 0644 /etc/systemd/system/smail.service
  chown root:root /etc/systemd/system/smail.service
fi

if [ -f "$systemd_src/smail-admin.service" ]; then
  sed -e "s|WorkingDirectory=[^ ]*|WorkingDirectory=$target_dir|g" \
      -e "s|ExecStart=[^ ]*/smail-admin|ExecStart=$target_dir/smail-admin|g" \
      "$systemd_src/smail-admin.service" > /etc/systemd/system/smail-admin.service
  chmod 0644 /etc/systemd/system/smail-admin.service
  chown root:root /etc/systemd/system/smail-admin.service
fi

# 7. 重载 systemd 并重新启动 Smail
echo ">>> 重载 systemd 配置并重启 Smail 服务..."
systemctl daemon-reload
systemctl enable smail.service >/dev/null 2>&1 || true
systemctl restart smail.service

# 8. 检查运行状态
sleep 1
if systemctl is-active --quiet smail.service; then
  echo "=========================================================="
  echo " Smail 平滑升级成功！"
  echo " 升级程序写入目录: $target_dir"
  echo " 数据库、SSL 证书及域名配置均完整保留。"
  echo " 服务状态: active (running)"
  echo "=========================================================="
else
  echo "警告：Smail 服务启动异常，请检查服务日志: journalctl -u smail -n 20 --no-pager" >&2
  exit 1
fi