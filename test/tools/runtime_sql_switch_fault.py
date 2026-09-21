"""在隔离应用的真实 SQL 提交与根配置替换边界终止进程。"""

import argparse
import hashlib
import http.client
import json
import os
from pathlib import Path
import re
import signal
import subprocess
import sys
import threading
import time

from runtime_interruption import assert_scope
from runtime_migration import clear_pending_restart, login, run_probe, service_state
from runtime_sql import SQLDatabase
from runtime_sql_migration import (
    assert_active, assert_connections, connection_count, fingerprint,
    history_rows, table_rows, target_payload,
)


def wait_until(predicate, timeout=60):
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        if predicate():
            return
        time.sleep(0.02)
    raise AssertionError('联合故障验收等待超时')


def prepare(root, password, target):
    client, _ = login(root, password)
    generation = clear_pending_restart(client)
    preview = client.success('/api/v1/database/migrations/preview', {
        'expectedGeneration': generation, 'target': target_payload(target),
    })
    # 令牌与会话只在探针内存中；就绪标记不包含秘密。
    (root.parent / 'switch-ready').touch(exist_ok=False)
    wait_until(lambda: (root.parent / 'switch-gate').is_file())
    try:
        client.request('/api/v1/database/migrations', {
            'id': preview['migration']['id'], 'token': preview['token'],
        })
    except (http.client.RemoteDisconnected, ConnectionResetError):
        return {'request_disconnected': True}
    raise AssertionError('未观察到连接被终止，普通响应或超时不能算进程终止验收')


def traced_tasks(pid, tracer):
    tasks = list(Path('/proc/' + str(pid) + '/task').iterdir())
    assert tasks
    return all('TracerPid:\t' + str(tracer) + '\n' in (task / 'status').read_text() for task in tasks)


def stopped_tasks(pid):
    tasks = list(Path('/proc/' + str(pid) + '/task').iterdir())
    assert tasks
    for task in tasks:
        status = (task / 'status').read_text()
        state = next(line.split()[1] for line in status.splitlines() if line.startswith('State:'))
        if state not in ('T', 't'):
            return False
    return True


def boundary(root, trace, phase, source, target):
    manifests = list((root / 'data/transaction').glob('*/manifest.json'))
    if len(manifests) != 1:
        return None
    manifest = json.loads(manifests[0].read_text())
    if manifest['phase'] != 'sections_committed':
        return None
    text = trace.read_text() if trace.exists() else ''
    # 同名归档也会延迟；只有目录描述符明确指向活动配置的调用才可命中。
    pattern = r'renameat(?:2)?\(\d+</opt/smail/config>, "[^"\n]+", \d+</opt/smail/config>, "config.json"'
    if not re.search(pattern, text):
        return None
    expected = source if phase == 'before-root' else target
    if json.loads((root / 'config/config.json').read_text())['database']['name'] != expected.settings['name']:
        return None
    return manifest


def assert_committed(source, target):
    source_mail, target_mail = table_rows(source, 'email'), table_rows(target, 'email')
    assert source_mail and source_mail == target_mail, '独立连接没有看到完整已提交邮件'
    source_history, target_history = history_rows(source), history_rows(target)
    assert source_history and set(source_history).issubset(target_history), '目标缺少已提交配置历史'
    return {'committed_mail_rows': len(target_mail), 'committed_history_rows': len(target_history),
            'mail_equal': True, 'source_history_preserved': True}


def verify_switch_fault(root, unit, password, result, source, target, phase):
    assert_scope(root, unit)
    assert phase in ('before-root', 'after-root') and source.kind == target.kind
    assert source.settings['name'] != target.settings['name']
    details = result['migration'] = {'passed': False, 'fault_phase': phase, 'database_type': source.kind}
    settings = {'source': source.settings, 'target': target.settings}
    outcome = {}
    thread = trace = None
    frozen = False
    trace_path = root.parent / 'switch-trace.log'

    def request():
        try:
            outcome['probe'] = run_probe(root, unit, password, 'switch-request', settings,
                                         probe_script='runtime_sql_switch_fault.py')
        except Exception as error:
            outcome['error'] = type(error).__name__

    try:
        initial = service_state(unit)
        pid = initial['MainPID']
        assert pid.isdigit() and int(pid) > 0
        assert Path('/proc/' + pid + '/root/opt/smail/smail').samefile(root / 'smail')
        assert Path('/proc/' + pid + '/ns/net').readlink() != Path('/proc/self/ns/net').readlink()
        thread = threading.Thread(target=request, daemon=True)
        thread.start()
        wait_until(lambda: (root.parent / 'switch-ready').exists())
        assert thread.is_alive() and service_state(unit)['MainPID'] == pid
        assert_active(root, source)
        assert list((root / 'data/transaction').iterdir()) == []
        with (root.parent / 'switch-trace.stderr').open('w') as errors:
            delay = 'enter' if phase == 'before-root' else 'exit'
            trace = subprocess.Popen(['strace', '-f', '-ttt', '-T', '-yy',
                '-e', 'trace=rename,renameat,renameat2', '-P', 'config.json',
                '-e', 'inject=rename,renameat,renameat2:delay_' + delay + '=6s',
                '-o', str(trace_path), '-p', pid], stdout=subprocess.DEVNULL, stderr=errors)
            wait_until(lambda: traced_tasks(pid, trace.pid), timeout=10)
            (root.parent / 'switch-gate').touch(exist_ok=False)
            wait_until(lambda: boundary(root, trace_path, phase, source, target), timeout=55)
            assert thread.is_alive() and trace.poll() is None
            subprocess.run(['systemctl', 'kill', '--kill-whom=all', '--signal=SIGSTOP', unit], check=True, timeout=15)
            frozen = True
            wait_until(lambda: stopped_tasks(pid), timeout=5)
            manifest = boundary(root, trace_path, phase, source, target)
            assert manifest is not None, '冻结后故障边界已经变化'
            details['committed'] = assert_committed(source, target)
            active, offline = (source, target) if phase == 'before-root' else (target, source)
            assert_active(root, active)
            offline_digest = fingerprint(offline)
            details['observed'] = {'manifest_phase': manifest['phase'],
                'from_generation': manifest['fromGeneration'], 'to_generation': manifest['toGeneration'],
                'root_database': active.settings['name'], 'active_root_syscall_observed': True,
                'all_application_threads_stopped': True}
            subprocess.run(['systemctl', 'kill', '--kill-whom=all', '--signal=SIGKILL', unit], check=True, timeout=15)
            frozen = False
            wait_until(lambda: service_state(unit)['MainPID'] == '0')
            killed = service_state(unit)
            assert killed['ExecMainStatus'] == '9'
            trace.wait(timeout=15)
            thread.join(timeout=35)
            assert not thread.is_alive() and 'error' not in outcome and outcome['probe']['request_disconnected']
            assert connection_count(source) == connection_count(target) == 0
            assert fingerprint(offline) == offline_digest
            target.revoke_creation()
            recovery_settings = {'source': offline.settings, 'target': active.settings}
            subprocess.run(['systemctl', 'start', unit], check=True, timeout=45)
            details['recovered'] = run_probe(root, unit, password, 'recovered', recovery_settings)
            recovered = service_state(unit)
            assert recovered['MainPID'] not in ('0', pid)
            assert recovered['InvocationID'] != initial['InvocationID']
            details['connections'] = assert_connections(offline, active)
            assert fingerprint(offline) == offline_digest
            assert list((root / 'data/transaction').iterdir()) == []
            assert list((root / 'data/transaction-staging').iterdir()) == []
            # 再次从磁盘启动，证明恢复结果可重复使用，不只依赖首次恢复的内存状态。
            subprocess.run(['systemctl', 'restart', unit], check=True, timeout=45)
            details['second_restart'] = run_probe(root, unit, password, 'process', recovery_settings)
            final = service_state(unit)
            assert final['MainPID'] not in ('0', pid, recovered['MainPID'])
            assert fingerprint(offline) == offline_digest
            assert_connections(offline, active)
            details.update(passed=True, offline_database_unchanged=True,
                           temporary_creation_privilege_revoked=True,
                           killed_state=killed, recovered_state=recovered, final_state=final,
                           trace_sha256=hashlib.sha256(trace_path.read_bytes()).hexdigest())
    finally:
        # 异常路径不让带有延迟器的迁移继续执行；仅停止已经核验的隔离应用。
        if trace is not None and trace.poll() is None:
            subprocess.run(['systemctl', 'kill', '--kill-whom=all', '--signal=SIGKILL', unit],
                           capture_output=True, timeout=15)
            try:
                trace.wait(timeout=15)
            except subprocess.TimeoutExpired:
                trace.send_signal(signal.SIGTERM)
                trace.wait(timeout=10)
        elif frozen:
            subprocess.run(['systemctl', 'kill', '--kill-whom=all', '--signal=SIGKILL', unit],
                           capture_output=True, timeout=15)
        if thread is not None:
            thread.join(timeout=90)
        (root.parent / 'migration-result.json').write_text(
            json.dumps(details, ensure_ascii=False, indent=2), encoding='utf-8')


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('root', type=Path)
    parser.add_argument('phase', choices=('switch-request',))
    args = parser.parse_args()
    assert os.geteuid() != 0
    password = sys.stdin.readline().strip()
    assert len(password) == 12
    settings = json.loads(sys.stdin.readline())
    assert set(settings) == {'source', 'target'}
    source, target = (SQLDatabase(settings[key]) for key in ('source', 'target'))
    assert source.kind == target.kind and source.settings['name'] != target.settings['name']
    result = prepare(args.root, password, target)
    result['passed'] = True
    (args.root.parent / ('migration-' + args.phase + '.json')).write_text(
        json.dumps(result, ensure_ascii=False, indent=2), encoding='utf-8')
    print(json.dumps(result, ensure_ascii=False))


if __name__ == '__main__':
    main()
