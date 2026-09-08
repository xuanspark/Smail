"""使用最新二进制验证空目录正式启动，保留并恢复现有模拟部署。"""

import hashlib
import http.client
import json
import os
from pathlib import Path
import shutil
import socket
import subprocess
import time


ROOT = Path(__file__).resolve().parents[2]
RUNTIME = ROOT / "yunx-test"
BUSINESS = {
    "credential.key": "credentials",
    "external-email-encryption.key": "external-email-encryption",
    "external-email-fingerprint.key": "external-email-fingerprint",
}


def checked(path):
    absolute = path.resolve()
    if not absolute.is_relative_to(ROOT) or absolute == ROOT or path.is_symlink():
        raise RuntimeError("测试路径越过项目边界或为链接")
    return absolute


def digest(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def main():
    for service_port in (80, 443, 5201):
        with socket.socket() as probe:
            probe.settimeout(0.5)
            if probe.connect_ex(("127.0.0.1", service_port)) == 0:
                raise RuntimeError("请先停止模拟部署的前后端，不能移动运行中的部署")
    receipt = json.loads((ROOT / "out" / "build-state.json").read_text(encoding="utf-8"))
    for item in receipt["files"]:
        if item["target"] and digest(RUNTIME / item["target"]) != item["sha256"]:
            raise RuntimeError("部署二进制与最新构建清单不一致")
    port = 15201
    with socket.socket() as probe:
        probe.bind(("127.0.0.1", port))
    record = checked(ROOT / "test-log" / time.strftime("ssl-startup-%Y%m%d-%H%M%S"))
    record.mkdir()
    saved = checked(record / "existing-runtime")
    fixture = checked(record / "fresh-runtime")
    checked(RUNTIME)
    # 必须由调用方先停止本项目两个服务；部署入口自身也会拒绝覆盖运行程序。
    RUNTIME.rename(saved)
    result = {"productionArguments": ["-p", str(port)], "starts": []}
    process = None
    try:
        RUNTIME.mkdir()
        for name in ("smail.exe", "smail-admin.exe"):
            shutil.copy2(saved / name, RUNTIME / name)
        env = dict(os.environ)
        env["SMAIL_ROOT"] = str(RUNTIME)
        env["SMAIL_SETUP_HOST"] = "127.0.0.1"
        for name in ("SMAIL_HTTP_PORT_OVERRIDE", "SMAIL_HTTP_TLS_MODE_OVERRIDE", "SMAIL_SETUP_TOKEN", "SMAIL_SETUP_ALLOWED_CIDRS"):
            env.pop(name, None)
        first = None
        for attempt in range(2):
            with (record / f"startup-{attempt + 1}.log").open("wb") as log:
                process = subprocess.Popen(
                    [str(RUNTIME / "smail.exe"), "-p", str(port)], cwd=RUNTIME,
                    env=env, stdout=log, stderr=subprocess.STDOUT,
                    creationflags=subprocess.CREATE_NO_WINDOW if os.name == "nt" else 0,
                )
                ready = False
                for _ in range(100):
                    if process.poll() is not None:
                        raise RuntimeError("正式启动提前退出，请检查测试日志")
                    connection = http.client.HTTPConnection("127.0.0.1", port, timeout=1)
                    try:
                        connection.request("GET", "/")
                        response = connection.getresponse()
                        ready = response.status == 200
                        response.read()
                    except OSError:
                        pass
                    finally:
                        connection.close()
                    if ready:
                        break
                    time.sleep(0.1)
                if not ready:
                    raise RuntimeError("正式启动未进入初始化页面")
                for folder in ("config", "ssl", "data", "log"):
                    if not (RUNTIME / folder).is_dir():
                        raise RuntimeError("正式启动没有创建部署目录")
                directories = [str(p.relative_to(RUNTIME)) for p in sorted((RUNTIME / "ssl").iterdir()) if p.is_dir()]
                for folder in (*BUSINESS.values(), "dkim", "managed"):
                    if not (RUNTIME / "ssl" / folder).is_dir():
                        raise RuntimeError("主程序没有自动创建业务目录")
                if first is not None and directories != first:
                    raise RuntimeError("重复启动的业务目录结构发生变化")
                first = directories
                result["starts"].append({"httpStatus": 200, "sslDirectories": directories})
                process.terminate()
                process.wait(timeout=15)
                process = None
        result["binaryMatchesLatest"] = digest(RUNTIME / "smail.exe") == digest(saved / "smail.exe")
    finally:
        if process is not None and process.poll() is None:
            process.kill()
            process.wait(timeout=15)
        if RUNTIME.exists():
            checked(RUNTIME).rename(fixture)
        checked(saved).rename(RUNTIME)
    result["existingRuntimeRestored"] = True
    (record / "result.json").write_text(json.dumps(result, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    print("空目录正式启动和重复启动通过，现有部署已恢复：", record.relative_to(ROOT))


if __name__ == "__main__":
    main()
