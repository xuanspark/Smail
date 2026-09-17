"""资源预检的反例和部署策略回归；仅在批准的远程测试环境运行。"""

import configparser
import importlib.util
import io
import json
from pathlib import Path
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import patch


ROOT = Path(__file__).resolve().parents[2]
SPEC = importlib.util.spec_from_file_location('check_resources', ROOT / 'deploy/check_resources.py')
RESOURCES = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(RESOURCES)
MIB = RESOURCES.MIB


def snapshot():
    return {
        'stable': True, 'go_memory_bytes': 256 * MIB,
        'host_available_bytes': 1024 * MIB, 'host_total_bytes': 2048 * MIB,
        'oom': {'oom': 0, 'oom_kill': 0}, 'parents': [],
        'service': {
            'MemoryHigh': str(384 * MIB), 'MemoryMax': str(512 * MIB),
            'MemorySwapMax': '0', 'TasksMax': '256', 'OOMPolicy': 'stop',
            'KillMode': 'control-group', 'Restart': 'on-failure',
            'RestartUSec': '10s', 'StartLimitIntervalUSec': '5min', 'StartLimitBurst': '3',
            'TimeoutStopUSec': '1min 30s',
        },
        'kernel': {
            'memory.high': str(384 * MIB), 'memory.max': str(512 * MIB),
            'memory.current': str(128 * MIB), 'memory.swap.max': '0',
            'pids.max': '256', 'pids.current': '32', 'cpu.max': '100000 100000',
        },
    }


class ResourcePolicyTests(unittest.TestCase):
    def test_valid_budget_is_not_production_approval(self):
        result = RESOURCES.evaluate(snapshot(), 256 * MIB)
        self.assertTrue(result['resource_checks_passed'])
        self.assertFalse(result['production_approved'])
        self.assertEqual(result['remaining_growth_bytes'], 384 * MIB)
        self.assertEqual(result['failures'], [])

    def test_missing_and_unsafe_limits_fail_closed(self):
        cases = [
            ('kernel', 'memory.high', 'max'),
            ('kernel', 'memory.max', 'max'),
            ('kernel', 'memory.high', str(512 * MIB)),
            ('kernel', 'memory.high', '0'),
            ('kernel', 'memory.current', str(384 * MIB)),
            ('kernel', 'memory.swap.max', 'max'),
            ('kernel', 'memory.swap.max', str(MIB)),
            ('kernel', 'pids.max', 'max'),
            ('kernel', 'pids.max', '0'),
            ('kernel', 'pids.current', '256'),
            ('kernel', 'cpu.max', 'max 100000'),
            ('service', 'MemoryMax', 'infinity'),
            ('service', 'MemoryHigh', str(256 * MIB)),
            ('service', 'MemorySwapMax', 'infinity'),
            ('service', 'TasksMax', '512'),
            ('service', 'OOMPolicy', 'continue'),
            ('service', 'KillMode', 'process'),
            ('service', 'TimeoutStopUSec', '60s'),
            ('service', 'TimeoutStopUSec', 'infinity'),
            ('service', 'Restart', 'always'),
            ('service', 'RestartUSec', '100ms'),
            ('service', 'RestartUSec', 'infinity'),
            ('service', 'StartLimitIntervalUSec', '0'),
            ('service', 'StartLimitIntervalUSec', '5s'),
            ('service', 'StartLimitBurst', '0'),
            ('service', 'StartLimitBurst', '6'),
            ('oom', 'oom', 1),
            ('oom', 'oom_kill', 1),
        ]
        for section, key, value in cases:
            with self.subTest(section=section, key=key, value=value):
                candidate = snapshot()
                candidate[section][key] = value
                report = RESOURCES.evaluate(candidate, 256 * MIB)
                self.assertFalse(report['resource_checks_passed'])
                self.assertTrue(report['failures'])

    def test_process_budget_and_restart_race_fail_closed(self):
        for key, values in {
            'go_memory_bytes': [None, 0, 63 * MIB, 384 * MIB, 1024 * MIB],
            'stable': [False],
            'host_available_bytes': [128 * MIB, 512 * MIB],
            'host_total_bytes': [512 * MIB],
        }.items():
            for value in values:
                with self.subTest(key=key, value=value):
                    candidate = snapshot()
                    candidate[key] = value
                    self.assertFalse(RESOURCES.evaluate(candidate, 256 * MIB)['resource_checks_passed'])
        self.assertFalse(RESOURCES.evaluate(snapshot(), 0)['resource_checks_passed'])

    def test_ancestor_limits_cannot_be_ignored(self):
        baseline = {'memory.high': 'max', 'memory.max': 'max', 'memory.current': str(256 * MIB),
                    'pids.max': 'max', 'pids.current': '64', 'cpu.max': 'max 100000'}
        cases = [('memory.high', str(768 * MIB)), ('memory.max', str(768 * MIB)),
                 ('pids.max', '256'), ('cpu.max', '50000 100000')]
        for key, value in cases:
            with self.subTest(key=key):
                candidate = snapshot()
                limits = {**baseline, key: value}
                candidate['parents'] = [{'path': 'system.slice', 'limits': limits}]
                result = RESOURCES.evaluate(candidate, 256 * MIB)
                self.assertFalse(result['resource_checks_passed'])
                self.assertTrue(any('system.slice' in failure for failure in result['failures']))
        candidate = snapshot()
        candidate['parents'] = [{'path': 'system.slice', 'limits': baseline}]
        self.assertTrue(RESOURCES.evaluate(candidate, 256 * MIB)['resource_checks_passed'])

    def test_environment_parser_never_returns_credentials(self):
        environment = b'SMAIL_SECRET=must-not-appear\0GOMEMLIMIT=256MiB\0TOKEN=also-secret\0'
        self.assertEqual(RESOURCES.go_memory_limit(environment), 256 * MIB)
        for value in (b'', b'off', b'256MB', b'1.5GiB', b'secret', b'-1', b' 256MiB', b'\xff'):
            with self.subTest(value=value):
                self.assertIsNone(RESOURCES.go_memory_limit(b'GOMEMLIMIT=' + value + b'\0'))
        self.assertIsNone(RESOURCES.go_memory_limit(environment + b'GOMEMLIMIT=128MiB\0'))
        self.assertIsNone(RESOURCES.go_memory_limit(b'TOKEN=secret\0'))
        self.assertEqual(RESOURCES.go_memory_limit(b'GOMEMLIMIT=1024\0'), 1024)
        self.assertEqual(RESOURCES.go_memory_limit(b'GOMEMLIMIT=1GiB\0'), 1024 * MIB)

    def test_duration_and_cpu_parsing(self):
        self.assertEqual(RESOURCES.duration_usec('5min'), 300000000)
        self.assertEqual(RESOURCES.duration_usec('1min 2.5s'), 62500000)
        self.assertEqual(RESOURCES.duration_usec('500ms'), 500000)
        for value in ('infinity', 'junk10s', '10s junk', '-1s', ''):
            self.assertIsNone(RESOURCES.duration_usec(value))
        self.assertEqual(RESOURCES.cpu_quota('25000 100000'), 0.25)
        self.assertIsNone(RESOURCES.cpu_quota('max 100000'))
        for value in ('0 100000', '100000 0', '-1 100000', 'unknown'):
            with self.assertRaises(ValueError):
                RESOURCES.cpu_quota(value)

    def test_systemd_query_is_read_only_and_does_not_request_environment(self):
        output = '\n'.join(name + '=value' for name in RESOURCES.PROPERTIES)
        output += '\nEnvironment=TOKEN=must-not-appear'
        with patch.object(RESOURCES.subprocess, 'run', return_value=SimpleNamespace(stdout=output)) as run:
            result = RESOURCES.systemd_state()
        self.assertEqual(set(result), set(RESOURCES.PROPERTIES))
        self.assertNotIn('must-not-appear', json.dumps(result))
        command = run.call_args.args[0]
        self.assertEqual(command[:3], ['systemctl', 'show', 'smail.service'])
        self.assertNotIn('Environment', command[-1])
        self.assertNotIn('ExecStart', command[-1])
        self.assertNotIn('shell', run.call_args.kwargs)
        with patch.object(RESOURCES.subprocess, 'run', return_value=SimpleNamespace(stdout='MainPID=1')):
            with self.assertRaises(ValueError):
                RESOURCES.systemd_state()

    def test_collect_refuses_inactive_service_before_reading_process(self):
        with patch.object(RESOURCES, 'systemd_state', return_value={
            'MainPID': '0', 'ActiveState': 'inactive', 'SubState': 'dead',
        }):
            with self.assertRaises(ValueError):
                RESOURCES.collect()

    def collection_fixture(self):
        root = Path(tempfile.mkdtemp(prefix='smail-resource-'))
        base = root / 'sys/fs/cgroup'
        group = base / 'system.slice/smail.service'
        proc = root / 'proc/123'
        group.mkdir(parents=True)
        proc.mkdir(parents=True)
        for name, value in snapshot()['kernel'].items():
            (group / name).write_text(value, encoding='ascii')
        (group / 'memory.events').write_text('oom 0\noom_kill 0\n', encoding='ascii')
        (proc / 'cgroup').write_text('0::/system.slice/smail.service\n', encoding='ascii')
        (proc / 'environ').write_bytes(b'GOMEMLIMIT=256MiB\0TOKEN=must-not-appear\0')
        (root / 'proc/meminfo').write_text('MemAvailable: 1048576 kB\nMemTotal: 2097152 kB\n',
                                         encoding='ascii')
        state = {**snapshot()['service'], 'MainPID': '123', 'NRestarts': '0',
                 'InvocationID': 'fixture', 'ActiveState': 'active', 'SubState': 'running',
                 'ControlGroup': '/system.slice/smail.service'}

        def locate(value):
            path = Path(value)
            if str(path).startswith('/proc') or str(path).startswith('/sys/fs/cgroup'):
                return root / str(path).lstrip('/')
            return path

        return root, state, locate

    def test_collect_reads_kernel_and_only_the_go_environment(self):
        root, state, locate = self.collection_fixture()
        with patch.object(RESOURCES, 'systemd_state', side_effect=[state, state.copy()]), \
                patch.object(RESOURCES, 'Path', side_effect=locate):
            result = RESOURCES.collect()
        self.assertTrue(result['stable'])
        self.assertEqual(result['go_memory_bytes'], 256 * MIB)
        self.assertEqual(result['host_available_bytes'], 1024 * MIB)
        self.assertNotIn('must-not-appear', json.dumps(result))
        self.assertEqual([parent['path'] for parent in result['parents']], ['system.slice', '.'])
        self.assertTrue(RESOURCES.evaluate(result, 256 * MIB)['resource_checks_passed'])

    def test_collect_detects_service_restart(self):
        root, state, locate = self.collection_fixture()
        changed = {**state, 'InvocationID': 'new-process'}
        with patch.object(RESOURCES, 'systemd_state', side_effect=[state, changed]), \
                patch.object(RESOURCES, 'Path', side_effect=locate):
            result = RESOURCES.collect()
        self.assertFalse(result['stable'])
        self.assertFalse(RESOURCES.evaluate(result, 256 * MIB)['resource_checks_passed'])

    def test_collect_refuses_pid_in_another_group(self):
        root, state, locate = self.collection_fixture()
        (root / 'proc/123/cgroup').write_text('0::/system.slice/other.service\n', encoding='ascii')
        with patch.object(RESOURCES, 'systemd_state', return_value=state), \
                patch.object(RESOURCES, 'Path', side_effect=locate):
            with self.assertRaises(ValueError):
                RESOURCES.collect()

    def test_cli_redacts_collection_errors_and_fails(self):
        output = io.StringIO()
        with patch('sys.argv', ['check_resources.py', '--reserve-mib', '256']), \
                patch.object(RESOURCES.os, 'name', 'posix'), \
                patch.object(RESOURCES.os, 'geteuid', return_value=0, create=True), \
                patch.object(RESOURCES, 'collect', side_effect=OSError('TOKEN=must-not-appear')), \
                patch('sys.stdout', output):
            status = RESOURCES.main()
        self.assertEqual(status, 1)
        self.assertNotIn('TOKEN', output.getvalue())
        self.assertFalse(json.loads(output.getvalue())['production_approved'])

    def test_cli_reports_success_without_production_approval(self):
        output = io.StringIO()
        with patch('sys.argv', ['check_resources.py', '--reserve-mib', '256']), \
                patch.object(RESOURCES.os, 'name', 'posix'), \
                patch.object(RESOURCES.os, 'geteuid', return_value=0, create=True), \
                patch.object(RESOURCES, 'collect', return_value=snapshot()), \
                patch('sys.stdout', output):
            self.assertEqual(RESOURCES.main(), 0)
        self.assertTrue(json.loads(output.getvalue())['resource_checks_passed'])
        self.assertFalse(json.loads(output.getvalue())['production_approved'])

    def test_deployment_unit_has_bounded_resources_and_restart_policy(self):
        unit = configparser.ConfigParser(interpolation=None)
        unit.read(ROOT / 'deploy/systemd/smail.service', encoding='utf-8')
        values = unit['Service']
        for key, value in {'MemoryHigh': '384M', 'MemoryMax': '512M', 'MemorySwapMax': '0',
                           'CPUQuota': '100%', 'TasksMax': '256', 'OOMPolicy': 'stop',
                           'KillMode': 'control-group', 'Restart': 'on-failure',
                           'RestartSec': '10s', 'TimeoutStopSec': '90s',
                           'MemoryAccounting': 'true', 'CPUAccounting': 'true',
                           'TasksAccounting': 'true'}.items():
            self.assertEqual(values[key], value)
        self.assertEqual(unit['Unit']['StartLimitIntervalSec'], '300s')
        self.assertEqual(unit['Unit']['StartLimitBurst'], '3')

    def test_installer_preserves_existing_environment_budget(self):
        source = (ROOT / 'deploy/install-systemd.sh').read_text(encoding='utf-8')
        guard = source.index('if [ ! -e /etc/smail/smail.env ]; then')
        end = source.index('\nfi', guard)
        assignment = source.index("'GOMEMLIMIT=256MiB'")
        self.assertTrue(guard < assignment < end)
        self.assertEqual(source.count('GOMEMLIMIT='), 1)
        self.assertIn('chmod 0600 /etc/smail/smail.env', source)
        self.assertIn('cp -p -- /etc/smail/smail.env "$backup_dir/smail.env"', source)


if __name__ == '__main__':
    unittest.main(verbosity=2)
