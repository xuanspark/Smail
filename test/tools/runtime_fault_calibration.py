"""在独立微型进程中校准系统调用延迟，不附加正式或测试应用。"""

import hashlib
import json
import os
from pathlib import Path
import signal
import subprocess
import sys
import time


CHILD = r'''
import json, os, sys, time
from pathlib import Path
root = Path(sys.argv[1])
while not (root / 'gate').exists():
    time.sleep(0.01)
fd = os.open(root, os.O_RDONLY | os.O_DIRECTORY)
start = time.monotonic()
os.rename('unrelated.tmp', 'unrelated.json', src_dir_fd=fd, dst_dir_fd=fd)
(root / 'unrelated-result.json').write_text(json.dumps({'seconds': time.monotonic() - start}))
(root / 'ready').touch()
start = time.monotonic()
os.rename('candidate.tmp', 'config.json', src_dir_fd=fd, dst_dir_fd=fd)
(root / 'done.json').write_text(json.dumps({'seconds': time.monotonic() - start}))
os.close(fd)
'''


def wait_until(predicate, timeout=10):
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        if predicate():
            return
        time.sleep(0.02)
    raise AssertionError('校准等待超时')


def tracer_pid(pid):
    lines = Path('/proc/' + str(pid) + '/status').read_text().splitlines()
    return int(next(line.split(':')[1] for line in lines if line.startswith('TracerPid:')))


def calibrate(root, phase):
    assert phase in ('enter', 'exit')
    root.mkdir(mode=0o700)
    for name, value in [('config.json', 'old'), ('candidate.tmp', 'new'), ('unrelated.tmp', 'other')]:
        (root / name).write_text(value)
    child = subprocess.Popen([sys.executable, '-c', CHILD, str(root)],
                             stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    trace = None
    details = {'phase': phase, 'passed': False}
    try:
        with (root / 'trace.stderr').open('w') as errors:
            trace = subprocess.Popen(['strace', '-f', '-ttt', '-T', '-yy',
                '-e', 'trace=rename,renameat,renameat2', '-P', 'config.json',
                '-e', 'inject=rename,renameat,renameat2:delay_' + phase + '=3s',
                '-o', str(root / 'trace.log'), '-p', str(child.pid)],
                stdout=subprocess.DEVNULL, stderr=errors)
            wait_until(lambda: tracer_pid(child.pid) == trace.pid)
            wait_until(lambda: 'attached' in (root / 'trace.stderr').read_text())
            (root / 'gate').touch()
            wait_until(lambda: (root / 'ready').exists())
            unrelated = json.loads((root / 'unrelated-result.json').read_text())
            assert unrelated['seconds'] < 1, '路径过滤没有排除无关替换'
            time.sleep(0.4)
            observed = (root / 'config.json').read_text()
            assert observed == ('old' if phase == 'enter' else 'new'), '延迟边界不符合预期'
            assert 'renameat(' in (root / 'trace.log').read_text(), '延迟期间没有可观测的调用记录'
            assert not (root / 'done.json').exists(), '替换调用未处于延迟窗口'
            assert child.wait(timeout=10) == 0
            assert trace.wait(timeout=5) == 0
            duration = json.loads((root / 'done.json').read_text())['seconds']
            assert 2.5 <= duration <= 8
            assert (root / 'config.json').read_text() == 'new'
            details.update(passed=True, unrelated_seconds=unrelated['seconds'],
                           target_seconds=duration, observed_during_delay=observed,
                           child_exited=True, tracer_exited=True,
                           trace_sha256=hashlib.sha256((root / 'trace.log').read_bytes()).hexdigest())
    finally:
        # 只回收本函数亲自创建的子进程，不接收外部 PID，也不删除任何证据。
        if child.poll() is None:
            child.kill()
            child.wait(timeout=10)
        if trace is not None and trace.poll() is None:
            trace.send_signal(signal.SIGTERM)
            trace.wait(timeout=10)
        (root / 'result.json').write_text(json.dumps(details, indent=2), encoding='utf-8')
    return details


def main():
    assert os.geteuid() == 0
    root = Path('/var/tmp') / ('smail-review-fault-calibration-' + time.strftime('%Y%m%d-%H%M%S'))
    root.mkdir(mode=0o700)
    result = {'root': str(root), 'passed': False}
    try:
        result['strace_version'] = subprocess.check_output(['strace', '-V'], text=True).splitlines()[0]
        result['ptrace_scope'] = Path('/proc/sys/kernel/yama/ptrace_scope').read_text().strip()
        result['cases'] = [calibrate(root / phase, phase) for phase in ('enter', 'exit')]
        result['passed'] = True
    finally:
        (root / 'result.json').write_text(json.dumps(result, indent=2), encoding='utf-8')
        print(json.dumps(result, indent=2), flush=True)


if __name__ == '__main__':
    main()
