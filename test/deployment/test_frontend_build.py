"""维护窗口恢复逻辑回归，不触碰真实服务。"""

from pathlib import Path
import subprocess
import sys
import unittest
from unittest.mock import call, patch


sys.path.insert(0, str(Path(__file__).resolve().parents[1] / 'tools'))
import build_frontend_review as review


class RestoreServicesTest(unittest.TestCase):
    unit = 'smail-frontend-build-20260918-010000'

    def test_stop_build_before_restoring_services(self):
        with patch.object(review.subprocess, 'run') as run:
            self.assertEqual(review.restore_services(self.unit), [])
        self.assertEqual(run.call_args_list, [
            call(['systemctl', 'stop', self.unit], timeout=90, check=True),
            call(['systemctl', 'start', 'mysql.service'], check=True, timeout=90),
            call(['systemctl', 'start', 'Scert.service'], check=True, timeout=90),
        ])

    def test_attempt_remaining_restore_after_database_failure(self):
        error = subprocess.CalledProcessError(1, 'systemctl')
        with patch.object(review.subprocess, 'run', side_effect=[None, error, None]) as run:
            self.assertEqual(review.restore_services(self.unit), [
                {'service': 'mysql.service', 'error': 'CalledProcessError'},
            ])
        self.assertEqual(run.call_args_list[-1].args[0], ['systemctl', 'start', 'Scert.service'])

    def test_site_timeout_is_reported(self):
        error = subprocess.TimeoutExpired('systemctl', 90)
        with patch.object(review.subprocess, 'run', side_effect=[None, None, error]):
            self.assertEqual(review.restore_services(self.unit), [
                {'service': 'Scert.service', 'error': 'TimeoutExpired'},
            ])

    def test_reject_unrelated_service_targets(self):
        for name in ('nginx', 'smail.service', self.unit + ' other', '../mysql.service'):
            with self.subTest(name=name), patch.object(review.subprocess, 'run') as run:
                with self.assertRaises(AssertionError):
                    review.restore_services(name)
                run.assert_not_called()

    def test_stop_timeout_does_not_skip_restore(self):
        error = subprocess.TimeoutExpired('systemctl', 90)
        with patch.object(review.subprocess, 'run', side_effect=[error, None, None, None]) as run:
            self.assertEqual(review.restore_services(self.unit), [
                {'service': self.unit, 'error': 'TimeoutExpired'},
            ])
        self.assertEqual(run.call_args_list[1].args[0], [
            'systemctl', 'kill', '--kill-whom=all', '--signal=SIGKILL', self.unit,
        ])
        self.assertEqual([item.args[0] for item in run.call_args_list[-2:]], [
            ['systemctl', 'start', 'mysql.service'], ['systemctl', 'start', 'Scert.service'],
        ])

    def test_missing_build_unit_is_not_a_restore_failure(self):
        error = subprocess.CalledProcessError(5, 'systemctl')
        with patch.object(review.subprocess, 'run', side_effect=[error, None, None]) as run, \
                patch.object(review, 'run', return_value='not-found'):
            self.assertEqual(review.restore_services(self.unit), [])
        self.assertEqual(run.call_count, 3)

    def test_existing_unit_stop_failure_is_reported(self):
        error = subprocess.CalledProcessError(1, 'systemctl')
        with patch.object(review.subprocess, 'run', side_effect=[error, None, None, None]), \
                patch.object(review, 'run', return_value='loaded'):
            self.assertEqual(review.restore_services(self.unit), [
                {'service': self.unit, 'error': 'CalledProcessError'},
            ])

    def test_stop_and_kill_failures_do_not_skip_remaining_restores(self):
        failures = [OSError(), subprocess.TimeoutExpired('systemctl', 30),
                    subprocess.CalledProcessError(1, 'systemctl'), None]
        with patch.object(review.subprocess, 'run', side_effect=failures) as run:
            self.assertEqual(review.restore_services(self.unit), [
                {'service': self.unit, 'error': 'OSError'},
                {'service': self.unit, 'error': 'TimeoutExpired', 'action': 'kill'},
                {'service': 'mysql.service', 'error': 'CalledProcessError'},
            ])
        self.assertEqual(run.call_args_list[-1].args[0], ['systemctl', 'start', 'Scert.service'])

    def test_unknown_unit_state_cannot_hide_stop_failure(self):
        error = subprocess.CalledProcessError(1, 'systemctl')
        with patch.object(review.subprocess, 'run', side_effect=[error, None, None, None]), \
                patch.object(review, 'run', side_effect=OSError()):
            self.assertEqual(review.restore_services(self.unit), [
                {'service': self.unit, 'error': 'CalledProcessError'},
            ])


class BuildBudgetTest(unittest.TestCase):
    def test_full_budget_is_accepted(self):
        review.check_memory_budget(1024 * 1024)
        self.assertEqual(review.SYSTEM_RESERVE_MIB, 128)
        self.assertLess(review.NODE_HEAP_MIB, review.BUILD_MEMORY_MIB)

    def test_insufficient_reserve_is_rejected(self):
        with self.assertRaises(RuntimeError):
            review.check_memory_budget(1024 * 1024 - 1)


if __name__ == '__main__':
    unittest.main()
