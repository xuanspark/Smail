"""只读核对运行目录迁移前后的文件及 SQLite 内容，不输出用户数据或密钥。"""

import argparse
import hashlib
import json
import sqlite3
from pathlib import Path


CONFIG_FILES = {"config.json", "smtp.json", "email.json", "ssl.json", "plugins.json"}
LEGACY_FILES = {"config.dev.json", "config_mysql.json", "config.json.legacy"}


def digest_file(path):
    digest = hashlib.sha256()
    with path.open("rb") as stream:
        for chunk in iter(lambda: stream.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def database_snapshot(path):
    # 必须停服调用。只读连接不会创建空库，不使用会忽略 WAL 的 immutable 模式。
    connection = sqlite3.connect(path.resolve().as_uri() + "?mode=ro", uri=True)
    try:
        connection.execute("PRAGMA query_only = ON")
        connection.execute("BEGIN")
        if connection.execute("PRAGMA integrity_check").fetchall() != [("ok",)]:
            raise RuntimeError("数据库完整性检查失败")
        tables = {}
        for (name,) in connection.execute(
            "SELECT name FROM sqlite_master WHERE type='table' ORDER BY name"
        ).fetchall():
            quoted = '"' + name.replace('"', '""') + '"'
            rows = []
            for row in connection.execute(f"SELECT * FROM {quoted}"):
                encoded = [
                    {"blob": value.hex()} if isinstance(value, bytes) else value
                    for value in row
                ]
                payload = json.dumps(encoded, ensure_ascii=False, separators=(",", ":"))
                rows.append(hashlib.sha256(payload.encode("utf-8")).hexdigest())
            tables[name] = {
                "rows": len(rows),
                "sha256": hashlib.sha256("".join(sorted(rows)).encode("ascii")).hexdigest(),
            }
        return {"integrity": "ok", "tables": tables}
    finally:
        connection.close()


def snapshot(root, layout):
    config = root / "config"
    files = {}
    databases = {}
    source = config if layout == "old" else root / "data"
    for path in sorted(source.rglob("*")):
        if path.is_symlink():
            raise RuntimeError("拒绝跟随运行目录符号链接")
        if not path.is_file():
            continue
        relative = path.relative_to(source).as_posix()
        # 完整迁移前备份是新建的独立副本，已单独核对，不属于待迁移的原始集合。
        if layout == "new" and path.relative_to(source).parts[0] == "runtime-migration-backups":
            continue
        if layout == "old":
            if relative in CONFIG_FILES:
                key = "config/" + relative
            elif relative in LEGACY_FILES:
                key = "data/legacy-config/" + relative
            else:
                key = "data/" + relative
        else:
            key = "data/" + relative
        files[key] = {"size": path.stat().st_size, "sha256": digest_file(path)}
        if path.suffix in {".db", ".sqlite", ".sqlite3"}:
            databases[key] = database_snapshot(path)
    if layout == "new":
        if {path.name for path in config.iterdir()} != CONFIG_FILES:
            raise RuntimeError("config 目录必须恰好包含五个配置文件")
        for name in sorted(CONFIG_FILES):
            path = config / name
            files["config/" + name] = {"size": path.stat().st_size, "sha256": digest_file(path)}
    generation = json.loads((config / "config.json").read_text(encoding="utf-8"))["generation"]
    return {"generation": generation, "files": files, "databases": databases}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--root", required=True, type=Path)
    parser.add_argument("--layout", required=True, choices=("old", "new"))
    parser.add_argument("--output", required=True, type=Path)
    parser.add_argument("--baseline", type=Path)
    args = parser.parse_args()
    result = snapshot(args.root.resolve(strict=True), args.layout)
    if args.baseline:
        before = json.loads(args.baseline.read_text(encoding="utf-8"))
        if before != result:
            raise RuntimeError("迁移前后文件或数据库内容不一致，请保留现场")
    # 不覆盖既有证据。
    with args.output.open("x", encoding="utf-8") as stream:
        json.dump(result, stream, ensure_ascii=False, indent=2)
        stream.write("\n")
    print(f"核对通过：配置代次 {result['generation']}，文件 {len(result['files'])} 个，数据库 {len(result['databases'])} 个")


if __name__ == "__main__":
    main()
