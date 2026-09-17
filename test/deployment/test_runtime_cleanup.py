'''隔离验收收尾回归，模拟异常，不停止真实服务。'''

from pathlib import Path
import subprocess
import sys
import unittest
from unittest.mock import patch


sys.path.insert(0, str(Path(__file__).resolve().parents[1] / 'tools'))
import runtime_review as review


class RuntimeCleanupTest(unittest.TestCase):
    app = 'smail-runtime-review-20260918-020000'
    probe = 'smail-runtime-probe-20260918-020000'
    proxy = 'smail-runtime-nginx-20260918-020000'

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
