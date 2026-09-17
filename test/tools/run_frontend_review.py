"""在远程受限服务逐文件运行前端测试，保留日志并拒绝空测试通过。"""

import argparse
import json
import os
from pathlib import Path
import re
import signal
import subprocess
import time


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('directory')
    parser.add_argument('--worker', action='store_true')
    args = parser.parse_args()
    root = Path(args.directory).resolve(strict=True)
    assert root.parent == Path('/var/tmp') and root.name.startswith('smail-review-')
    if not args.worker:
        assert os.geteuid() == 0
        memory = dict(line.split(':', 1) for line in Path('/proc/meminfo').read_text().splitlines())
        assert int(memory['MemAvailable'].split()[0]) >= 256 * 1024, '可用内存不足'
        command = [
            'systemd-run', '--wait', '--pipe', '--unit=smail-frontend-tests-' + time.strftime('%Y%m%d-%H%M%S'),
            '-p', 'User=smail', '-p', 'Group=smail', '-p', 'MemoryMax=128M', '-p', 'MemorySwapMax=0',
            '-p', 'TasksMax=128', '-p', 'CPUQuota=25%', '-p', 'RuntimeMaxSec=600s',
            '-p', 'TimeoutStopSec=10s', '-p', 'KillMode=control-group', '-p', 'OOMPolicy=stop',
            '-p', 'PrivateTmp=yes', '-p', 'PrivateNetwork=yes', '-p', 'BindPaths=' + str(root),
            '-p', 'ProtectSystem=strict', '-p', 'ProtectHome=yes', '-p', 'InaccessiblePaths=/opt/smail /run/smail',
            '-p', 'NoNewPrivileges=yes', '-p', 'ReadWritePaths=' + str(root), '-p', 'WorkingDirectory=' + str(root),
            'python3', str(root / 'run_frontend_review.py'), str(root), '--worker',
        ]
        return subprocess.run(command).returncode
    assert os.geteuid() != 0
    node = root / 'node-v22.23.2-linux-x64/bin/node'
    tests = sorted((root / 'test/frontend').glob('*.test.mjs'))
    assert tests, '未发现前端测试'
    output = root / ('results-' + time.strftime('%Y%m%d-%H%M%S'))
    output.mkdir(mode=0o700)
    environment = {name: value for name, value in os.environ.items() if not name.startswith('SMAIL_')}
    environment['NODE_OPTIONS'] = '--max-old-space-size=48'
    results = []
    for test in tests:
        log = output / (test.name + '.log')
        started = time.monotonic()
        with log.open('w') as stream:
            process = subprocess.Popen([str(node), '--test', str(test)], cwd=root, env=environment,
                                       stdout=stream, stderr=subprocess.STDOUT, start_new_session=True)
            try:
                code = process.wait(timeout=90)
            except subprocess.TimeoutExpired:
                os.killpg(process.pid, signal.SIGKILL)
                process.wait()
                code = 124
        text = log.read_text(encoding='utf-8', errors='replace')
        counts = {key: int(value) for key, value in re.findall(r'^# (tests|pass|fail|skipped) (\d+)$', text, re.M)}
        code = code or (0 if counts.get('tests', 0) > 0 and counts.get('fail') == 0 else 2)
        result = dict(test=test.name, code=code, seconds=round(time.monotonic() - started, 2), **counts)
        results.append(result)
        (output / 'results.json').write_text(json.dumps(results, ensure_ascii=False, indent=2), encoding='utf-8')
        print(json.dumps(result, ensure_ascii=False), flush=True)
        if code:
            print(text, flush=True)
    print(json.dumps({'files': len(results), 'passed': sum(item.get('pass', 0) for item in results),
                      'failed': sum(item.get('fail', 0) for item in results),
                      'skipped': sum(item.get('skipped', 0) for item in results)}, ensure_ascii=False), flush=True)
    print('测试记录：' + str(output), flush=True)
    return int(any(item['code'] for item in results))


if __name__ == '__main__':
    raise SystemExit(main())
