'''隔离验收收尾回归，模拟异常，不停止真实服务。'''

from pathlib import Path
from contextlib import closing
import subprocess
import sqlite3
import sys
import tempfile
import unittest
from unittest.mock import patch


sys.path.insert(0, str(Path(__file__).resolve().parents[1] / 'tools'))
import runtime_review as review


class RuntimeCleanupTest(unittest.TestCase):
    app = 'smail-runtime-review-20260918-020000'
    probe = 'smail-runtime-probe-20260918-020000'
    proxy = 'smail-runtime-nginx-20260918-020000'

    def test_memory_profiles_keep_explicit_native_reserve(self):
        for hard, soft in ((128, 64), (192, 64), (192, 128), (256, 192)):
            review.validate_memory_profile(hard, soft)
        for hard, soft in ((128, 128), (192, 192), (128, 256), (64, 64), (1024, 64)):
            with self.subTest(hard=hard, soft=soft), self.assertRaises(ValueError):
                review.validate_memory_profile(hard, soft)

    def test_go_resource_environment_does_not_record_other_variables(self):
        self.assertEqual(review.go_environment(b'PASSWORD=not-for-logs\0GOMEMLIMIT=128MiB\0GOMAXPROCS=1\0'),
                         {'GOMEMLIMIT': '128MiB', 'GOMAXPROCS': '1'})
        self.assertEqual(review.go_environment(b'PASSWORD=not-for-logs\0'), {})
        with self.assertRaises(ValueError):
            review.go_environment(b'GOMEMLIMIT=64MiB\0GOMEMLIMIT=128MiB\0')

    def test_single_database_integrity_and_legacy_rejection(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / 'data').mkdir()
            with closing(sqlite3.connect(root / 'data/smail.db')) as database:
                database.execute('CREATE TABLE config_records (id TEXT)')
            self.assertEqual(review.database_integrity(root), {'smail.db': [('ok',)]})
            (root / 'data/smail-state.db').touch()
            with self.assertRaises(AssertionError):
                review.database_integrity(root)

    def test_missing_database_is_not_created_by_verifier(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / 'data').mkdir()
            with self.assertRaises(AssertionError):
                review.database_integrity(root)
            self.assertEqual(list((root / 'data').iterdir()), [])

    def test_missing_history_table_is_rejected(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / 'data').mkdir()
            with closing(sqlite3.connect(root / 'data/smail.db')) as database:
                database.execute('CREATE TABLE other (id TEXT)')
            with self.assertRaises(AssertionError):
                review.database_integrity(root)

    def test_nested_database_is_rejected(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / 'data').mkdir()
            with closing(sqlite3.connect(root / 'data/smail.db')) as database:
                database.execute('CREATE TABLE config_records (id TEXT)')
            (root / 'data/archive').mkdir()
            (root / 'data/archive/old.db').touch()
            with self.assertRaises(AssertionError):
                review.database_integrity(root)
    def test_resources_then_stop_all(self):
        result = {}
        with patch.object(review.subprocess, 'check_output', return_value='Result=success') as stats, \
                patch.object(review, 'stop_test_units', return_value=[]) as stop:
            review.collect_and_stop(result, self.app, self.probe, self.proxy)
        self.assertEqual(stats.call_count, 2)
        stop.assert_called_once_with([self.proxy, self.probe, self.app])
        self.assertEqual(result['cleanup_errors'], [])
        self.assertEqual(result['resource_errors'], [])

    def test_resource_timeout_still_stops_all(self):
        result = {}
        with patch.object(review.subprocess, 'check_output', side_effect=subprocess.TimeoutExpired('show', 15)), \
                patch.object(review, 'stop_test_units', return_value=[]) as stop:
            review.collect_and_stop(result, self.app, self.probe, self.proxy)
        self.assertEqual(len(result['resource_errors']), 2)
        stop.assert_called_once_with([self.proxy, self.probe, self.app])

    def test_stop_timeout_and_kill_error_do_not_skip_remaining(self):
        success = subprocess.CompletedProcess([], 0, '', '')
        with patch.object(review.subprocess, 'run', side_effect=[
            subprocess.TimeoutExpired('stop', 25), OSError(), success, success,
        ]) as run:
            errors = review.stop_test_units([self.proxy, self.probe, self.app])
        self.assertEqual([item['action'] for item in errors], ['stop', 'kill'])
        self.assertEqual(run.call_args_list[-1].args[0], ['systemctl', 'stop', self.app])

    def test_missing_unit_is_not_failure(self):
        missing = subprocess.CompletedProcess([], 5, '', 'Unit not loaded.')
        with patch.object(review.subprocess, 'run', return_value=missing):
            self.assertEqual(review.stop_test_units([self.probe]), [])

    def test_stop_failure_attempts_kill_and_reports_failure(self):
        failed = subprocess.CompletedProcess([], 1, '', 'Failed to stop unit')
        success = subprocess.CompletedProcess([], 0, '', '')
        with patch.object(review.subprocess, 'run', side_effect=[failed, success]) as run:
            errors = review.stop_test_units([self.app])
        self.assertEqual(errors[0]['error'], 'CalledProcessError')
        self.assertEqual(run.call_args_list[1].args[0], [
            'systemctl', 'kill', '--kill-whom=all', '--signal=SIGKILL', self.app,
        ])

    def test_reject_business_units_before_any_stop(self):
        for name in ('nginx', 'smail.service', self.app + ' other', '../mysql.service'):
            with self.subTest(name=name), patch.object(review.subprocess, 'run') as run:
                with self.assertRaises(AssertionError):
                    review.stop_test_units([self.app, name])
                run.assert_not_called()


if __name__ == '__main__':
    unittest.main()
