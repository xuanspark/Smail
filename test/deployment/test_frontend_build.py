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
            call(['systemctl', 'stop', self.unit], timeout=90, check=False),
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


if __name__ == '__main__':
    unittest.main()
