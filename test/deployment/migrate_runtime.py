"""停服后迁移隔离模拟部署，保留源文件和完整备份，不清空业务数据库。"""

import argparse
import hashlib
import json
import os
from pathlib import Path
import shutil
import sqlite3
import subprocess
from datetime import datetime


ROOT = Path(__file__).resolve().parents[2]
TARGET = ROOT / "yunx-test"
CONFIG_NAMES = {f"{name}.json" for name in ("config", "smtp", "email", "ssl", "plugins")}


def safe_tree(directory):
    for current, directories, files in os.walk(directory, followlinks=False):
        for name in directories + files:
            item = Path(current) / name
            if item.is_symlink() or item.is_junction():
                raise RuntimeError("迁移集合包含链接，已停止")
    if directory.is_symlink() or directory.is_junction():
        raise RuntimeError("迁移目录不能是链接")


def digest(filename):
    return hashlib.file_digest(filename.open("rb"), "sha256").hexdigest()


def copy_verified(source, target):
    if target.exists():
        if target.is_file() and digest(source) == digest(target):
            return
        raise RuntimeError(f"目标冲突，拒绝覆盖：{target.relative_to(ROOT)}")
    target.parent.mkdir(parents=True, exist_ok=True)
    shutil.copy2(source, target)
    if digest(source) != digest(target):
        raise RuntimeError("文件复制摘要不一致")


def copy_tree(source, target):
    safe_tree(source)
    for item in source.rglob("*"):
        if item.is_file():
            copy_verified(item, target / item.relative_to(source))


def sqlite_snapshot(source, target):
    with sqlite3.connect(source.as_uri() + "?mode=ro", uri=True) as src:
        if src.execute("PRAGMA integrity_check").fetchone()[0] != "ok":
            raise RuntimeError("源 SQLite 完整性检查未通过")
        with sqlite3.connect(target) as dst:
            src.backup(dst)
            if dst.execute("PRAGMA integrity_check").fetchone()[0] != "ok":
                raise RuntimeError("目标 SQLite 完整性检查未通过")
            tables = [row[0] for row in src.execute("SELECT name FROM sqlite_master WHERE type='table'")]
            for name in tables:
                quoted = '"' + name.replace('"', '""') + '"'
                if src.execute(f"SELECT count(*) FROM {quoted}").fetchone() != dst.execute(f"SELECT count(*) FROM {quoted}").fetchone():
                    raise RuntimeError("SQLite 表记录数量不一致")
    return len(tables)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--apply", action="store_true")
    args = parser.parse_args()
    safe_tree(TARGET)
    for name in ("config", "data", "ssl"):
        safe_tree(ROOT / name)
    if {item.name for item in (ROOT / "config").iterdir()} != CONFIG_NAMES:
        raise RuntimeError("源配置目录不止五个指定配置")
    if any((ROOT / "data" / "transaction").iterdir()):
        raise RuntimeError("源目录存在未恢复配置事务，不能迁移")
    if any((TARGET / name).exists() for name in ("config", "ssl", "data")):
        raise RuntimeError("目标已有运行数据，拒绝重复迁移")
    if not args.apply:
        print("迁移集合检查通过。先停止前后端，再使用 --apply。")
        return
    # 再次核对端口，避免复制期间仍有配置写入。
    result = subprocess.run([
        "powershell.exe", "-NoProfile", "-Command",
        "if (Get-NetTCPConnection -State Listen -LocalPort 5201 -ErrorAction SilentlyContinue) { exit 1 }"
    ], capture_output=True)
    if result.returncode:
        raise RuntimeError("5201 后端仍在运行，拒绝迁移")
    stamp = datetime.now().strftime("%Y%m%d-%H%M%S")
    backup = ROOT / "test-log" / f"deployment-migration-{stamp}"
    backup.mkdir(parents=True, exist_ok=False)
    for name in ("config", "data", "ssl"):
        copy_tree(ROOT / name, backup / name)
    for name in ("config", "ssl", "data", "log"):
        (TARGET / name).mkdir(parents=True, exist_ok=True)
    copy_tree(backup / "config", TARGET / "config")
    copy_tree(backup / "data" / "ssl", TARGET / "ssl")
    copy_tree(backup / "ssl", TARGET / "ssl")
    copy_tree(backup / "data" / "dkim", TARGET / "ssl" / "dkim")
    for name in ("credential.key", "external-email-encryption.key", "external-email-fingerprint.key"):
        copy_verified(backup / "data" / name, TARGET / "ssl" / name)
    copy_verified(backup / "data" / "config-audit-v2.jsonl", TARGET / "log" / "config-audit-v2.jsonl")
    databases = {}
    for source in (ROOT / "data").glob("*.db"):
        databases[source.name] = {"tables": sqlite_snapshot(source, TARGET / "data" / source.name), "integrity": "ok"}
    env = {**os.environ, "SMAIL_ROOT": str(TARGET)}
    result = subprocess.run([
        str(ROOT / "out" / "backend" / "smail-config.exe"), "--root", str(TARGET),
        "--deployment-history", str(ROOT), "--apply"
    ], env=env, capture_output=True)
    (backup / "import.stdout.log").write_bytes(result.stdout)
    (backup / "import.stderr.log").write_bytes(result.stderr)
    if result.returncode:
        raise RuntimeError("配置历史导入失败；保留原目录、目标和备份供排查，不启动服务")
    report = {"source": str(ROOT), "target": str(TARGET), "backup": str(backup), "databases": databases,
              "files": {str(item.relative_to(TARGET)): digest(item) for item in TARGET.rglob("*") if item.is_file()}}
    (backup / "receipt.json").write_text(json.dumps(report, ensure_ascii=False, indent=2), encoding="utf-8")
    print(f"迁移完成；一致性验收记录：{backup.relative_to(ROOT)}/receipt.json")


if __name__ == "__main__":
    main()
