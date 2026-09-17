"""通过受限 systemd 临时服务执行远程测试，不复用生产服务进程。"""

import argparse
import os
from pathlib import Path
import subprocess
import time


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('directory')
    parser.add_argument('--only', action='append', default=[])
    parser.add_argument('--run', default='')
    parser.add_argument('--memory-mib', type=int, choices=(64, 96, 128, 192), default=192)
    args = parser.parse_args()
    root = Path(args.directory).resolve(strict=True)
    assert os.geteuid() == 0
    assert root.parent == Path('/var/tmp') and root.name.startswith('smail-review-')
    memory = dict(line.split(':', 1) for line in Path('/proc/meminfo').read_text().splitlines())
    assert int(memory['MemAvailable'].split()[0]) >= (args.memory_mib + 128) * 1024, '可用内存不足，停止增加测试负载'
    unit = 'smail-tests-' + time.strftime('%Y%m%d-%H%M%S')
    command = [
        'systemd-run', '--wait', '--pipe', '--unit=' + unit,
        '--property=User=smail', '--property=Group=smail',
        '--property=MemoryMax=' + str(args.memory_mib) + 'M', '--property=MemorySwapMax=0',
        '--property=TasksMax=128', '--property=CPUQuota=25%',
        '--property=RuntimeMaxSec=1800s', '--property=TimeoutStopSec=10s',
        '--property=KillMode=control-group', '--property=OOMPolicy=stop',
        '--property=PrivateTmp=yes', '--property=PrivateNetwork=yes',
        '--property=BindPaths=' + str(root),
        '--property=ProtectSystem=strict', '--property=ProtectHome=yes',
        '--property=InaccessiblePaths=/opt/smail /run/smail',
        '--property=NoNewPrivileges=yes', '--property=ReadWritePaths=' + str(root),
        '--property=WorkingDirectory=' + str(root),
        'python3', str(root / 'run_review_tests.py'),
    ]
    for directory in args.only:
        command.extend(['--only', directory])
    if args.run:
        command.extend(['--run', args.run])
    print('测试服务：' + unit, flush=True)
    return subprocess.run(command).returncode


if __name__ == '__main__':
    raise SystemExit(main())
