"""Formal cloud acceptance tool single-database boundary tests."""

from contextlib import closing
from pathlib import Path
import sqlite3
import sys
import tempfile
import unittest


TOOLS = Path(__file__).resolve().parents[1] / 'tools'
sys.path.insert(0, str(TOOLS))
import deploy_review
import internal_smtp_check
import restore_review
import verify_review


class FormalReviewDatabaseTest(unittest.TestCase):
    modules = (deploy_review, internal_smtp_check, restore_review, verify_review)

    def make_database(self, root):
        data = root / 'data'
        data.mkdir()
        with closing(sqlite3.connect(data / 'smail.db')) as database:
            database.execute('CREATE TABLE marker (id INTEGER)')

    def test_all_formal_tools_accept_one_database(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            self.make_database(root)
            for module in self.modules:
                with self.subTest(module=module.__name__):
                    self.assertEqual(module.database_path(root), root / 'data/smail.db')

    def test_all_formal_tools_reject_legacy_database(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            self.make_database(root)
            (root / 'data/smail-state.db').touch()
            for module in self.modules:
                with self.subTest(module=module.__name__), self.assertRaises(AssertionError):
                    module.database_path(root)

    def test_all_formal_tools_reject_nested_database(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            self.make_database(root)
            nested = root / 'data' / 'archive'
            nested.mkdir()
            (nested / 'old.db').touch()
            for module in self.modules:
                with self.subTest(module=module.__name__), self.assertRaises(AssertionError):
                    module.database_path(root)

    def test_all_formal_tools_reject_missing_database_without_creating_one(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / 'data').mkdir()
            for module in self.modules:
                with self.subTest(module=module.__name__), self.assertRaises(AssertionError):
                    module.database_path(root)
            self.assertEqual(list((root / 'data').iterdir()), [])


if __name__ == '__main__':
    unittest.main()