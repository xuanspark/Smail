"""在授权维护窗口构建前端，退出或超时后恢复原先运行的服务。"""

import argparse
import json
import os
from pathlib import Path
import re
import subprocess
import time

from run_frontend_review import dependency_bindings


PAUSED = ('Scert.service', 'mysql.service')
PROTECTED = ('smail.service', 'nginx.service')
BUILD_MEMORY_MIB = 896
NODE_HEAP_MIB = 640
SYSTEM_RESERVE_MIB = 128


def run(*args):
    return subprocess.check_output(args, text=True, timeout=90).strip()


def states():
    return {name: dict(line.split('=', 1) for line in run(
        'systemctl', 'show', name, '--property=ActiveState,MainPID,NRestarts').splitlines())
        for name in PAUSED + PROTECTED}


def restore_services(build_unit):
    assert re.fullmatch(r'smail-frontend-build-[0-9]{8}-[0-9]{6}', build_unit)
    errors = []
    try:
        subprocess.run(['systemctl', 'stop', build_unit], timeout=90, check=True)
    except Exception as error:
        # 预检拒绝时构建单元尚不存在；其他停止失败必须记录并尝试终止残留进程。
        missing = False
        if isinstance(error, subprocess.CalledProcessError):
            try:
                missing = run('systemctl', 'show', build_unit, '--property=LoadState', '--value') == 'not-found'
            except Exception:
                pass
        if not missing:
            errors.append({'service': build_unit, 'error': type(error).__name__})
            try:
                subprocess.run(['systemctl', 'kill', '--kill-whom=all', '--signal=SIGKILL', build_unit],
                               timeout=30, check=True)
            except Exception as kill_error:
                errors.append({'service': build_unit, 'error': type(kill_error).__name__, 'action': 'kill'})
    for name in reversed(PAUSED):
        try:
            subprocess.run(['systemctl', 'start', name], check=True, timeout=90)
        except Exception as error:
            errors.append({'service': name, 'error': type(error).__name__})
    return errors


def check_memory_budget(available_kib):
    required_kib = (BUILD_MEMORY_MIB + SYSTEM_RESERVE_MIB) * 1024
    if available_kib < required_kib:
        raise RuntimeError('暂停服务后仍不足以保留 128 MiB 系统预算')


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('directory')
    parser.add_argument('--pause-other-services', action='store_true')
    parser.add_argument('--worker', action='store_true')
    parser.add_argument('--build-unit')
    parser.add_argument('--restore-unit')
    args = parser.parse_args()
    root = Path(args.directory).resolve(strict=True)
    assert os.geteuid() == 0 and run('hostname') == 'ikun2'
    assert root.parent == Path('/var/tmp') and root.name.startswith('smail-review-')
    if args.restore_unit:
        return int(bool(restore_services(args.restore_unit)))
    assert args.pause_other_services, '必须先获得暂停其他服务的授权'
    before = states()
    assert all(item['ActiveState'] == 'active' for item in before.values())
    live = json.loads(Path('/opt/smail/config/config.json').read_text())
    assert live['database']['type'] == 'sqlite', '线上 Smail 依赖外部数据库时不允许暂停 MySQL'
    bindings = dependency_bindings(root)
    assert not (root / 'out/frontend').exists(), '已有构建结果，必须先保留证据再执行'
    if not args.worker:
        stamp = time.strftime('%Y%m%d-%H%M%S')
        unit = 'smail-build-window-' + stamp
        build_unit = 'smail-frontend-build-' + stamp
        script = Path(__file__).resolve(strict=True)
        assert script.stat().st_uid == 0 and script.parent.stat().st_uid == 0
        assert script.stat().st_mode & 0o022 == 0 and script.parent.stat().st_mode & 0o022 == 0
        print('维护窗口单元：' + unit, flush=True)
        # 由独立 systemd 单元负责恢复，SSH 断开或工作进程被终止也不会取消恢复钩子。
        return subprocess.run([
            'systemd-run', '--wait', '--pipe', '--unit=' + unit,
            '-p', 'RuntimeMaxSec=900s', '-p', 'TimeoutStopSec=480s',
            '-p', 'KillMode=control-group', '-p', 'UMask=0077', '-p', 'LimitCORE=0',
            '-p', 'ExecStopPost=/usr/bin/python3 ' + str(script) + ' ' + str(root)
                  + ' --restore-unit ' + build_unit,
            'python3', str(script), str(root),
            '--pause-other-services', '--worker', '--build-unit', build_unit,
        ]).returncode

    stamp = time.strftime('%Y%m%d-%H%M%S')
    unit = args.build_unit
    assert unit and re.fullmatch(r'smail-frontend-build-[0-9]{8}-[0-9]{6}', unit)
    result = {'before': before, 'unit': unit, 'started': time.strftime('%Y-%m-%dT%H:%M:%S%z'),
              'build_memory_mib': BUILD_MEMORY_MIB, 'node_heap_mib': NODE_HEAP_MIB,
              'system_reserve_mib': SYSTEM_RESERVE_MIB}
    try:
        for name in PAUSED:
            subprocess.run(['systemctl', 'stop', name], check=True, timeout=90)
        memory = dict(line.split(':', 1) for line in Path('/proc/meminfo').read_text().splitlines())
        result['available_kib'] = int(memory['MemAvailable'].split()[0])
        check_memory_budget(result['available_kib'])
        node = root / 'node-v22.23.2-linux-x64/bin/node'
        command = [
            'systemd-run', '--wait', '--pipe', '--unit=' + unit,
            '-p', 'User=smail', '-p', 'Group=smail', '-p', f'MemoryMax={BUILD_MEMORY_MIB}M', '-p', 'MemorySwapMax=0',
            '-p', 'CPUQuota=75%', '-p', 'TasksMax=128', '-p', 'RuntimeMaxSec=600s',
            '-p', 'TimeoutStopSec=10s', '-p', 'KillMode=control-group', '-p', 'OOMPolicy=stop',
            '-p', 'PrivateTmp=yes', '-p', 'PrivateNetwork=yes', '-p', 'ProtectSystem=strict',
            '-p', 'ProtectHome=yes', '-p', 'NoNewPrivileges=yes', '-p', 'LimitCORE=0',
            '-p', 'InaccessiblePaths=/opt/smail /run/smail', '-p', 'BindPaths=' + str(root),
            '-p', 'ReadWritePaths=' + str(root), '-p', 'WorkingDirectory=' + str(root / 'frontend'),
            *[argument for target in bindings for argument in ('-p', 'BindReadOnlyPaths=' + target)],
            f'--setenv=NODE_OPTIONS=--max-old-space-size={NODE_HEAP_MIB}', '--setenv=HOME=' + str(root / 'build-home'),
            '/bin/sh', '-c',
            'set -eu\n' + str(node) + ' node_modules/vite/bin/vite.js build\n'
            + str(node) + ' ../deploy/prepare-frontend.mjs\n',
        ]
        with (root / ('frontend-build-' + stamp + '.txt')).open('w') as output:
            result['build_code'] = subprocess.run(command, stdout=output, stderr=subprocess.STDOUT,
                                                  timeout=660).returncode
        result['resources'] = run('systemctl', 'show', unit,
                                  '--property=Result,ExecMainStatus,MemoryPeak,MemorySwapPeak')
        assert result['build_code'] == 0, '前端构建失败，保留日志并恢复服务'
    except Exception as error:
        result['error'] = type(error).__name__
        raise
    finally:
        # 先恢复数据库，再恢复依赖数据库的站点；构建成功与否均执行。
        result['restore_errors'] = restore_services(unit)
        result['after'] = states()
        result['protected_unchanged'] = all(result['after'][name] == before[name] for name in PROTECTED)
        result['restored'] = all(result['after'][name]['ActiveState'] == 'active' for name in PAUSED)
        result['finished'] = time.strftime('%Y-%m-%dT%H:%M:%S%z')
        (root / ('build-window-' + stamp + '.json')).write_text(
            json.dumps(result, ensure_ascii=False, indent=2), encoding='utf-8')
        print(json.dumps(result, ensure_ascii=False, indent=2), flush=True)
    assert result['restored'] and result['protected_unchanged'] and not result['restore_errors']
    return 0


if __name__ == '__main__':
    raise SystemExit(main())
