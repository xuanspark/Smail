"""验证前端审查依赖的只读挂载范围，不启动本机应用。"""

import importlib.util
from pathlib import Path
import unittest
from unittest.mock import patch


source = Path(__file__).resolve().parents[1] / 'tools' / 'run_frontend_review.py'
spec = importlib.util.spec_from_file_location('frontend_review', source)
review = importlib.util.module_from_spec(spec)
spec.loader.exec_module(review)


class DependencyBindingsTest(unittest.TestCase):
    root = Path('/var/tmp/smail-review-current')

    def bindings(self, targets):
        with patch.object(Path, 'resolve', side_effect=map(Path, targets)):
            with patch.object(Path, 'is_dir', return_value=True):
                return review.dependency_bindings(self.root)

    def test_local_dependencies_need_no_extra_mount(self):
        self.assertEqual(self.bindings([
            str(self.root / 'node-v22.23.2-linux-x64'),
            str(self.root / 'frontend/node_modules'),
        ]), [])

    def test_shared_dependencies_are_explicit(self):
        targets = ['/var/tmp/smail-review-previous/node',
                   '/var/tmp/smail-review-previous/frontend/node_modules']
        self.assertEqual(self.bindings(targets), sorted(targets))

    def test_duplicate_mounts_are_combined(self):
        target = '/var/tmp/smail-review-previous/dependencies'
        self.assertEqual(self.bindings([target, target]), [target])

    def test_unsafe_targets_are_rejected(self):
        for target in ('/opt/smail', '/run/smail', '/var/tmp/unrelated/node',
                       '/var/tmp/smail-review-previous',
                       '/var/tmp/smail-review-previous/node:other',
                       '/var/tmp/smail-review-previous/node other'):
            with self.subTest(target=target), self.assertRaises(ValueError):
                self.bindings([target])

    def test_missing_dependency_fails_before_start(self):
        with patch.object(Path, 'resolve', side_effect=FileNotFoundError):
            with self.assertRaises(FileNotFoundError):
                review.dependency_bindings(self.root)

    def test_file_dependency_is_rejected(self):
        with patch.object(Path, 'resolve', return_value=self.root / 'node'):
            with patch.object(Path, 'is_dir', return_value=False):
                with self.assertRaises(ValueError):
                    review.dependency_bindings(self.root)


if __name__ == '__main__':
    unittest.main()
