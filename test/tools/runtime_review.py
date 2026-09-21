"""在 47 服务器创建空数据副本并进行受限验收，不停止线上服务或删除文件。"""

import argparse
from contextlib import closing
import hashlib
import json
import os
from pathlib import Path
import re
import secrets
import shutil
import sqlite3
import subprocess
import time


def run(*args):
    return subprocess.check_output(args, text=True).strip()


def service_state():
    return {name: run('systemctl', 'show', name, '--property=MainPID,NRestarts,ActiveState,InvocationID')
            for name in ('smail.service', 'nginx.service', 'mysql.service', 'Scert.service',
                         'fdm-studio.service', 'postgresql@18-main.service')}


def database_integrity(root):
    # 验收器不能创建数据库，也不能允许第二个状态库。
    data = root / 'data'
    assert not (data / 'smail-state.db').exists(), '单数据库实例生成了旧状态库'
    assert sorted(path.name for path in data.glob('*.db')) == ['smail.db']
    with closing(sqlite3.connect((data / 'smail.db').as_uri() + '?mode=ro', uri=True)) as database:
        integrity = database.execute('PRAGMA integrity_check').fetchall()
        assert database.execute("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='config_records'").fetchone()[0] == 1
    assert integrity == [('ok',)]
    return {'smail.db': integrity}


def stop_test_units(names):
    assert all(re.fullmatch(r'smail-runtime-(review|probe|nginx|netns)-[0-9]{8}-[0-9]{6}', name)
               for name in names), '只能停止本轮隔离测试单元'
    errors = []
    for name in names:
        try:
            stopped = subprocess.run(['systemctl', 'stop', name], capture_output=True,
                                     text=True, timeout=25)
            if stopped.returncode and 'not loaded' not in stopped.stderr:
                raise subprocess.CalledProcessError(stopped.returncode, 'systemctl')
        except (OSError, subprocess.SubprocessError) as error:
            errors.append({'unit': name, 'action': 'stop', 'error': type(error).__name__})
            try:
                subprocess.run(['systemctl', 'kill', '--kill-whom=all', '--signal=SIGKILL', name],
                               check=True, capture_output=True, timeout=15)
            except (OSError, subprocess.SubprocessError) as kill_error:
                errors.append({'unit': name, 'action': 'kill', 'error': type(kill_error).__name__})
    return errors


def collect_and_stop(result, unit, probe_unit, proxy_unit=None):
    result['resource_errors'] = []
    try:
        for key, name in [('resources', unit)] + ([('nginx_resources', proxy_unit)] if proxy_unit else []):
            try:
                result[key] = subprocess.check_output([
                    'systemctl', 'show', name,
                    '--property=MemoryPeak,MemoryCurrent,MemorySwapPeak,Result,ExecMainStatus,NRestarts',
                ], text=True, timeout=15).strip()
            except (OSError, subprocess.SubprocessError) as error:
                result['resource_errors'].append({'unit': name, 'error': type(error).__name__})
    finally:
        names = ([proxy_unit] if proxy_unit else []) + [probe_unit, unit]
        result['cleanup_errors'] = stop_test_units(names)


def migration_kind(source, target, enabled, interrupted):
    if target is None:
        return source
    if not enabled or interrupted or target not in ('sqlite', 'mysql', 'postgres'):
        raise ValueError('跨引擎目标必须用于独立 SQL 迁移验收')
    return target


def stop_database_bridges(bridges, result):
    errors = result.setdefault('cleanup_errors', [])
    closed = []
    for port, bridge in reversed(list(bridges.items())):
        try:
            bridge.close()
            closed.append(port)
        except Exception as error:
            errors.append({'action': 'database_bridge', 'port': port, 'error': type(error).__name__})
    result['database_bridge_ports_stopped'] = sorted(closed)
    result['database_bridge_stopped'] = bool(bridges) and len(closed) == len(bridges)


def validate_memory_profile(hard_mib, soft_mib):
    if hard_mib not in (128, 192, 256) or soft_mib not in (64, 128, 192):
        raise ValueError('不支持的隔离内存配置')
    if soft_mib + 64 > hard_mib:
        raise ValueError('Go 内存目标与进程硬上限之间必须保留至少 64 MiB')


def go_environment(raw):
    # 只保留本轮资源控制项，不能把其他环境变量写入证据。
    result = {}
    for item in raw.split(b'\0'):
        name, separator, value = item.partition(b'=')
        if separator and name in (b'GOMEMLIMIT', b'GOMAXPROCS'):
            if name.decode('ascii') in result:
                raise ValueError('Go 资源环境项重复')
            result[name.decode('ascii')] = value.decode('ascii')
    return result


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--main-sha', required=True)
    parser.add_argument('--binary', type=Path, default=Path('/opt/smail/smail'))
    parser.add_argument('--nginx-template', type=Path)
    parser.add_argument('--memory-max-mib', type=int, choices=(128, 192, 256), default=128)
    parser.add_argument('--go-memory-limit-mib', type=int, choices=(64, 128, 192), default=64)
    parser.add_argument('--restore', action='store_true')
    parser.add_argument('--migration', action='store_true')
    parser.add_argument('--interruption', action='store_true')
    parser.add_argument('--switch-fault', choices=('before-root', 'after-root'))
    parser.add_argument('--database-type', choices=('sqlite', 'mysql', 'postgres'), default='sqlite')
    parser.add_argument('--migration-target-type', choices=('sqlite', 'mysql', 'postgres'))
    args = parser.parse_args()
    try:
        validate_memory_profile(args.memory_max_mib, args.go_memory_limit_mib)
        target_kind = migration_kind(args.database_type, args.migration_target_type, args.migration, args.interruption)
    except ValueError as error:
        parser.error(str(error))
    if args.interruption and not args.migration:
        parser.error('中断验收必须选择同引擎迁移独立场景')
    if args.switch_fault and (not args.migration or args.interruption or
                              args.database_type == 'sqlite' or target_kind != args.database_type):
        parser.error('联合切换故障必须使用独立的同引擎 SQL 迁移场景')
    if args.migration and (args.nginx_template or args.restore):
        parser.error('迁移进程验收仅使用直接 TLS 的独立场景')
    if args.restore and args.database_type != 'sqlite':
        parser.error('SQL 数据库备份恢复需要独立验收，不能套用 SQLite 冷备流程')
    assert os.geteuid() == 0 and run('hostname') == 'ikun2'
    os.umask(0o027)
    live = Path('/opt/smail')
    binary = args.binary.resolve(strict=True)
    assert binary == args.binary and binary.is_file()
    if binary != live / 'smail':
        assert binary.parent.parent == Path('/var/tmp') and binary.parent.name.startswith('smail-review-')
        assert binary.stat().st_uid == 0 and binary.stat().st_mode & 0o022 == 0
        assert binary.parent.stat().st_uid == 0 and binary.parent.stat().st_mode & 0o022 == 0
    with binary.open('rb') as stream:
        assert hashlib.file_digest(stream, 'sha256').hexdigest() == args.main_sha
        stream.seek(0)
        header = stream.read(20)
        assert header[:5] == b'\x7fELF\x02' and header[18:20] == b'\x3e\x00'
    with (live / 'smail').open('rb') as stream:
        live_sha = hashlib.file_digest(stream, 'sha256').hexdigest()
    memory = dict(line.split(':', 1) for line in Path('/proc/meminfo').read_text().splitlines())
    required_memory = args.memory_max_mib + (192 if args.nginx_template else 144)
    assert int(memory['MemAvailable'].split()[0]) >= required_memory * 1024, '可用内存不足，暂停隔离验收'
    stamp = time.strftime('%Y%m%d-%H%M%S')
    target = Path('/var/tmp') / ('smail-review-runtime-' + stamp)
    target.mkdir(mode=0o750)
    shutil.chown(target, user='smail', group='smail')
    root = target / 'instance'
    control = target / 'control'
    for directory in (root, control, root / 'ssl'):
        directory.mkdir(mode=0o750)
        shutil.chown(directory, user='smail', group='smail')
    shutil.copy2(binary, root / 'smail')
    (root / 'smail').chmod(0o755)
    shutil.copy2(Path(__file__).with_name('runtime_probe.py'), target / 'runtime_probe.py')
    (target / 'runtime_probe.py').chmod(0o640)
    shutil.chown(target / 'runtime_probe.py', user='root', group='smail')
    if args.migration:
        shutil.copy2(Path(__file__).with_name('runtime_migration.py'), target / 'runtime_migration.py')
        (target / 'runtime_migration.py').chmod(0o640)
        shutil.chown(target / 'runtime_migration.py', user='root', group='smail')
    mixed = args.migration and args.database_type != target_kind and 'sqlite' in (args.database_type, target_kind)
    if args.database_type != 'sqlite' or mixed:
        shutil.copy2(Path(__file__).with_name('runtime_sql.py'), target / 'runtime_sql.py')
        (target / 'runtime_sql.py').chmod(0o640)
        shutil.chown(target / 'runtime_sql.py', user='root', group='smail')
        if args.migration:
            shutil.copy2(Path(__file__).with_name('runtime_sql_migration.py'), target / 'runtime_sql_migration.py')
            (target / 'runtime_sql_migration.py').chmod(0o640)
            shutil.chown(target / 'runtime_sql_migration.py', user='root', group='smail')
    if mixed:
        shutil.copy2(Path(__file__).with_name('runtime_mixed_migration.py'), target / 'runtime_mixed_migration.py')
        (target / 'runtime_mixed_migration.py').chmod(0o640)
        shutil.chown(target / 'runtime_mixed_migration.py', user='root', group='smail')
    if args.interruption or args.switch_fault:
        shutil.copy2(Path(__file__).with_name('runtime_interruption.py'), target / 'runtime_interruption.py')
        (target / 'runtime_interruption.py').chmod(0o640)
        shutil.chown(target / 'runtime_interruption.py', user='root', group='smail')
        if args.database_type != 'sqlite':
            shutil.copy2(Path(__file__).with_name('runtime_sql_interruption.py'), target / 'runtime_sql_interruption.py')
            (target / 'runtime_sql_interruption.py').chmod(0o640)
            shutil.chown(target / 'runtime_sql_interruption.py', user='root', group='smail')
    if args.switch_fault:
        shutil.copy2(Path(__file__).with_name('runtime_sql_switch_fault.py'), target / 'runtime_sql_switch_fault.py')
        (target / 'runtime_sql_switch_fault.py').chmod(0o640)
        shutil.chown(target / 'runtime_sql_switch_fault.py', user='root', group='smail')
    if args.nginx_template:
        shutil.copy2(Path(__file__).with_name('runtime_proxy.py'), target / 'runtime_proxy.py')
        (target / 'runtime_proxy.py').chmod(0o640)
        shutil.chown(target / 'runtime_proxy.py', user='root', group='smail')
    subprocess.run(['openssl', 'req', '-x509', '-newkey', 'rsa:2048', '-nodes', '-days', '2',
                    '-keyout', str(root / 'ssl/runtime.key'), '-out', str(root / 'ssl/runtime.crt'),
                    '-subj', '/CN=mail.runtime.invalid', '-addext',
                    'subjectAltName=DNS:runtime.invalid,DNS:*.runtime.invalid'],
                   check=True, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    for name in ('runtime.key', 'runtime.crt'):
        path = root / 'ssl' / name
        path.chmod(0o600)
        shutil.chown(path, user='smail', group='smail')
    assert not (root / 'data').exists() and not (root / 'config.json').exists()
    before = service_state()
    unit = 'smail-runtime-review-' + stamp
    probe_unit = 'smail-runtime-probe-' + stamp
    proxy_unit = 'smail-runtime-nginx-' + stamp
    result = {'target': str(target), 'unit': unit, 'main_sha256': args.main_sha,
              'memory_max_mib': args.memory_max_mib,
              'go_memory_limit_mib': args.go_memory_limit_mib, 'cpu_quota_percent': 25,
              'source': str(binary), 'live_sha256': live_sha, 'before': before,
              'topology': 'nginx-8001-8002' if args.nginx_template else 'direct-80-443'}
    service_env = []
    probe_args = []
    sql_fixture = None
    migration_target = None
    network_unit = None
    bridges = {}
    result['database_type'] = args.database_type
    if args.database_type != 'sqlite':
        from runtime_sql import SQLFixture
        sql_fixture = SQLFixture(args.database_type)
        result['sql_database'] = sql_fixture.settings['name']
    if args.migration and target_kind != 'sqlite':
        from runtime_sql_migration import MigrationTarget
        migration_target = MigrationTarget(target_kind)
        result['sql_target_database'] = migration_target.settings['name']
    if args.migration:
        result['migration_target_type'] = target_kind
    mixed_source = mixed_target = None
    if mixed:
        from runtime_mixed_migration import SQLiteDatabase
        mixed_source = sql_fixture or SQLiteDatabase(root, {'kind': 'sqlite', 'name': 'smail.db'})
        mixed_target = migration_target or SQLiteDatabase(root, {'kind': 'sqlite', 'name': 'migrated.db'})
    if args.nginx_template:
        from runtime_proxy import prepare_nginx
        prepare_nginx(target, root, args.nginx_template)
        result['nginx_template_sha256'] = hashlib.sha256(args.nginx_template.read_bytes()).hexdigest()
        service_env = ['--setenv=setup_port=8001', '--setenv=SMAIL_HTTP_PORT=8001',
                       '--setenv=SMAIL_HTTPS_PORT=8002']
        probe_args = ['--proxy']
    started = time.strftime('%Y-%m-%dT%H:%M:%S')
    password = secrets.token_urlsafe(9)
    try:
        if sql_fixture:
            sql_fixture.create()
            probe_args.append('--sql-stdin')
        network_args = ['--property=PrivateNetwork=yes']
        if migration_target:
            migration_target.create()
        if args.migration and (sql_fixture or migration_target):
            # 使用独立限时单元固定隔离网络，应用换进程后仍能访问本轮数据库桥接。
            network_unit = 'smail-runtime-netns-' + stamp
            subprocess.run([
                'systemd-run', '--unit=' + network_unit, '--property=Type=exec',
                '--property=User=smail', '--property=Group=smail', '--property=PrivateNetwork=yes',
                '--property=ProtectSystem=strict', '--property=ProtectHome=yes',
                '--property=NoNewPrivileges=yes', '--property=InaccessiblePaths=/opt/smail /run/smail',
                '--property=MemoryMax=8M', '--property=MemorySwapMax=0', '--property=TasksMax=8',
                '--property=RuntimeMaxSec=900s', '/usr/bin/sleep', '900',
            ], check=True)
            network_pid = run('systemctl', 'show', network_unit, '--property=MainPID', '--value')
            assert network_pid.isdigit() and int(network_pid) > 0
            assert Path('/proc/' + network_pid + '/ns/net').readlink() != Path('/proc/self/ns/net').readlink()
            network_args = ['--property=NetworkNamespacePath=/proc/' + network_pid + '/ns/net']
        subprocess.run([
            'systemd-run', '--unit=' + unit, '--property=Type=exec',
            '--property=User=smail', '--property=Group=smail',
            *network_args, '--property=PrivateTmp=yes',
            '--property=ProtectSystem=strict', '--property=ProtectHome=yes',
            '--property=NoNewPrivileges=yes', '--property=CapabilityBoundingSet=CAP_NET_BIND_SERVICE',
            '--property=AmbientCapabilities=CAP_NET_BIND_SERVICE',
            '--property=MemoryMax=' + str(args.memory_max_mib) + 'M',
            '--property=MemorySwapMax=0', '--property=CPUQuota=25%',
            '--property=TasksMax=128', '--property=RuntimeMaxSec=' + ('600s' if args.migration else '300s'),
            '--property=OOMPolicy=stop',
            '--property=KillMode=control-group', '--property=TimeoutStopSec=10s',
            '--property=BindPaths=' + str(root) + ':/opt/smail ' + str(control) + ':/run/smail',
            '--property=ReadWritePaths=/opt/smail /run/smail',
            '--property=WorkingDirectory=/opt/smail', '--setenv=SMAIL_ROOT=/opt/smail',
            '--setenv=GOMEMLIMIT=' + str(args.go_memory_limit_mib) + 'MiB',
            '--setenv=GOMAXPROCS=1', *service_env, '/opt/smail/smail',
        ], check=True)
        pid = run('systemctl', 'show', unit, '--property=MainPID', '--value')
        assert pid != '0'
        live_net = str(Path('/proc/self/ns/net').readlink())
        assert str(Path('/proc/' + pid + '/ns/net').readlink()) != live_net
        assert Path('/proc/' + pid + '/root/opt/smail/smail').stat().st_ino == (root / 'smail').stat().st_ino
        assert (root / 'smail').stat().st_ino != (live / 'smail').stat().st_ino
        result['go_environment'] = go_environment(Path('/proc/' + pid + '/environ').read_bytes())
        assert result['go_environment'] == {'GOMEMLIMIT': str(args.go_memory_limit_mib) + 'MiB', 'GOMAXPROCS': '1'}
        if sql_fixture or migration_target:
            from runtime_sql_bridge import DatabaseBridge
            ports = {fixture.port for fixture in (sql_fixture, migration_target) if fixture}
            for port in sorted(ports):
                bridges[port] = DatabaseBridge(int(pid), port)
            result['database_bridge_ports'] = sorted(bridges)
        if args.nginx_template:
            subprocess.run([
                'systemd-run', '--unit=' + proxy_unit, '--property=Type=exec',
                '--property=User=smail', '--property=Group=smail',
                '--property=NetworkNamespacePath=/proc/' + pid + '/ns/net',
                '--property=PrivateTmp=no', '--property=ProtectSystem=strict',
                '--property=ProtectHome=yes', '--property=NoNewPrivileges=yes',
                '--property=InaccessiblePaths=/opt/smail /run/smail /etc/nginx',
                '--property=ReadWritePaths=' + str(target / 'nginx'),
                '--property=CapabilityBoundingSet=CAP_NET_BIND_SERVICE',
                '--property=AmbientCapabilities=CAP_NET_BIND_SERVICE',
                '--property=MemoryMax=32M', '--property=MemorySwapMax=0',
                '--property=CPUQuota=25%', '--property=TasksMax=16',
                '--property=RuntimeMaxSec=300s', '--property=OOMPolicy=stop',
                '--property=KillMode=control-group', '--property=TimeoutStopSec=10s',
                '/usr/sbin/nginx', '-p', str(target / 'nginx'),
                '-c', str(target / 'nginx/nginx.conf'), '-g', 'daemon off;',
            ], check=True)
        probe = subprocess.run([
            'systemd-run', '--wait', '--pipe', '--unit=' + probe_unit,
            '--property=User=smail', '--property=Group=smail',
            '--property=NetworkNamespacePath=/proc/' + pid + '/ns/net',
            '--property=PrivateTmp=no', '--property=ProtectSystem=strict', '--property=ProtectHome=yes',
            '--property=NoNewPrivileges=yes', '--property=InaccessiblePaths=/opt/smail /run/smail',
            '--property=ReadWritePaths=' + str(target), '--property=MemoryMax=48M',
            '--property=MemorySwapMax=0', '--property=CPUQuota=25%', '--property=TasksMax=32',
            '--property=RuntimeMaxSec=240s', '--property=OOMPolicy=stop',
            '--property=KillMode=control-group', '--property=TimeoutStopSec=10s',
            'python3', str(target / 'runtime_probe.py'), str(root), '--live-net', live_net,
            '--password-stdin', *probe_args,
        ], input=password + '\n' + (json.dumps(sql_fixture.settings) + '\n' if sql_fixture else ''),
           text=True, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, timeout=270)
        (target / 'probe.log').write_text(probe.stdout, encoding='utf-8')
        result['probe_code'] = probe.returncode
        if (target / 'probe-result.json').exists():
            result['probe'] = json.loads((target / 'probe-result.json').read_text())
        assert probe.returncode == 0, '隔离验收失败，请检查保留的 probe.log'
        assert result['probe']['passed']
        with sqlite3.connect(f'file:{live / "data/smail.db"}?mode=ro', uri=True) as database:
            message_id = result['probe']['smtp']['message_id']
            assert database.execute('SELECT COUNT(*) FROM email WHERE msg_id=?', (message_id,)).fetchone()[0] == 0
        result['live_message_absent'] = True
        if args.migration:
            if args.interruption:
                if sql_fixture:
                    from runtime_sql_interruption import verify_interruption
                    verify_interruption(root, unit, password, result, sql_fixture)
                else:
                    from runtime_interruption import verify_interruption
                    verify_interruption(root, unit, password, result)
            if args.switch_fault:
                from runtime_sql_switch_fault import verify_switch_fault
                verify_switch_fault(root, unit, password, result, sql_fixture, migration_target, args.switch_fault)
            elif mixed:
                from runtime_mixed_migration import verify_migration
                verify_migration(root, unit, password, result, mixed_source, mixed_target)
            elif migration_target:
                from runtime_sql_migration import verify_migration
                verify_migration(root, unit, password, result, sql_fixture, migration_target)
            else:
                from runtime_migration import verify_migration
                verify_migration(root, unit, password, result)
    finally:
        try:
            collect_and_stop(result, unit, probe_unit, proxy_unit if args.nginx_template else None)
            result['after'] = service_state()
            with (live / 'smail').open('rb') as stream:
                result['live_unchanged'] = (result['after'] == before
                                            and hashlib.file_digest(stream, 'sha256').hexdigest() == live_sha)
            try:
                if mixed:
                    from runtime_mixed_migration import migration_integrity
                    result['integrity'] = migration_integrity(root, mixed_source, mixed_target)
                elif migration_target:
                    from runtime_sql_migration import assert_active
                    active_database = sql_fixture if args.switch_fault == 'before-root' else migration_target
                    assert_active(root, active_database)
                    result['integrity'] = active_database.integrity(root)
                elif args.migration:
                    from runtime_migration import migration_integrity
                    result['integrity'] = migration_integrity(root, interrupted=args.interruption)
                else:
                    result['integrity'] = sql_fixture.integrity(root) if sql_fixture else database_integrity(root)
            except (AssertionError, sqlite3.Error, RuntimeError, OSError, ValueError, subprocess.SubprocessError) as error:
                result['integrity'] = {'error': type(error).__name__}
        finally:
            result.setdefault('cleanup_errors', [])
            if bridges:
                stop_database_bridges(bridges, result)
            if sql_fixture:
                try:
                    sql_fixture.lock()
                    result['test_database_account_locked'] = True
                except Exception as error:
                    result['cleanup_errors'].append({'action': 'database_account', 'error': type(error).__name__})
            if migration_target:
                try:
                    migration_target.lock()
                    result['migration_target_account_locked'] = True
                except Exception as error:
                    result['cleanup_errors'].append({'action': 'migration_target_account', 'error': type(error).__name__})
            if network_unit:
                errors = stop_test_units([network_unit])
                result['cleanup_errors'].extend(errors)
                result['network_anchor_stopped'] = not errors
        (target / 'service.log').write_text(run('journalctl', '-u', unit, '--no-pager'), encoding='utf-8')
        kernel = run('journalctl', '-k', '--since', started, '--no-pager')
        result['oom_events'] = sum('oom-kill:constraint=' in line for line in kernel.splitlines())
        (target / 'result.json').write_text(json.dumps(result, ensure_ascii=False, indent=2), encoding='utf-8')
        print(json.dumps(result, ensure_ascii=False, indent=2))
    assert result['live_unchanged'] and result['oom_events'] == 0
    assert not result['cleanup_errors'], '测试单元未全部停止，需要核查保留的结果'
    assert not result['resource_errors'], '资源证据不完整，需要核查保留的结果'
    if mixed:
        assert result['migration']['passed'] and result['network_anchor_stopped']
        assert result['integrity']['config_records'] > 0
    elif sql_fixture:
        assert result['integrity'].get('sqlite_files') == 0 and result['integrity']['config_records'] > 0
        if args.migration:
            assert result['migration']['passed'] and result['network_anchor_stopped']
    elif args.migration:
        assert result['migration']['passed'] and result['integrity']['active_database'] == 'migrated.db'
    else:
        assert result['integrity'] == {'smail.db': [('ok',)]}
    if args.restore:
        from runtime_restore import verify_backup_restore
        restored = verify_backup_restore(root, result, password, args.nginx_template is not None)
        (target / 'restore-result.json').write_text(json.dumps(restored, ensure_ascii=False, indent=2), encoding='utf-8')
        print(json.dumps({'restore': restored}, ensure_ascii=False, indent=2))


if __name__ == '__main__':
    main()
