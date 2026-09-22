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
import runtime_review
from database_inventory import sqlite_inventory
from unittest.mock import patch


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


    def test_extra_sqlite_files_cannot_hide_by_name_or_location(self):
        for name in ('extra.db', 'STATE.DB', '.hidden.sqlite3', 'nested/extra.sqlite',
                     '../config/other.db', '../renamed.bin', 'old.db-wal', 'old.sqlite-shm'):
            with self.subTest(name=name), tempfile.TemporaryDirectory() as directory:
                root = Path(directory).resolve()
                self.make_database(root)
                path = root / 'data' / name
                path.parent.mkdir(parents=True, exist_ok=True)
                path.write_bytes(b'SQLite format 3\x00')
                for module in self.modules:
                    with self.subTest(module=module.__name__), self.assertRaises(AssertionError):
                        module.database_path(root)
                with self.assertRaises(AssertionError):
                    runtime_review.database_integrity(root)

    def test_expected_sqlite_sidecars_are_not_second_databases(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory).resolve()
            self.make_database(root)
            for suffix in ('-wal', '-shm', '-journal'):
                (root / 'data' / ('smail.db' + suffix)).touch()
            for module in self.modules:
                self.assertEqual(module.database_path(root), root / 'data/smail.db')

    def test_sql_mode_rejects_renamed_database_and_orphan_sidecars(self):
        for name in ('renamed.bin', 'data/.hidden.sqlite', 'data/old.db-wal'):
            with self.subTest(name=name), tempfile.TemporaryDirectory() as directory:
                root = Path(directory).resolve()
                (root / 'data').mkdir()
                self.assertEqual(sqlite_inventory(root, []), [])
                (root / name).write_bytes(b'SQLite format 3\x00')
                with self.assertRaises(AssertionError):
                    sqlite_inventory(root, [])

    def test_directory_links_are_rejected_before_reading_their_contents(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory).resolve()
            self.make_database(root)
            linked = root / 'data' / 'linked'
            linked.mkdir()
            original = Path.is_symlink
            with patch.object(Path, 'is_symlink', lambda path: path == linked or original(path)):
                with self.assertRaises(AssertionError):
                    sqlite_inventory(root, [root / 'data/smail.db'])

    def test_snapshot_and_restore_counts_close_their_connections(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory).resolve()
            self.make_database(root)
            source = root / 'data/smail.db'
            target = root / 'snapshot.db'
            deploy_review.snapshot(source, target)
            moved = root / 'snapshot-moved.db'
            target.rename(moved)
            with closing(sqlite3.connect(moved)) as database:
                self.assertEqual(database.execute('PRAGMA integrity_check').fetchone(), ('ok',))
            isolated = root / 'isolated'
            isolated.mkdir()
            self.make_database(isolated)
            self.assertEqual(restore_review.database_counts(isolated), {'smail.db': {'marker': 0}})
            (isolated / 'data/smail.db').rename(isolated / 'data/moved.db')


    def test_deployed_schema_requires_unified_configuration_history(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory).resolve()
            self.make_database(root)
            with closing(sqlite3.connect(root / 'data/smail.db')) as database:
                database.executescript('CREATE TABLE schema_migration (version INTEGER); '
                                      'INSERT INTO schema_migration VALUES (23); '
                                      'CREATE TABLE outbound_delivery (state TEXT); '
                                      "INSERT INTO outbound_delivery VALUES ('pending');")
                with self.assertRaises(AssertionError):
                    deploy_review.database_state(root)
                database.executescript('INSERT INTO schema_migration VALUES (24);')
                with self.assertRaises(sqlite3.OperationalError):
                    deploy_review.database_state(root)
                database.executescript('CREATE TABLE config_records (id TEXT);')
            self.assertEqual(deploy_review.database_state(root), (24, {'pending': 1}))


if __name__ == '__main__':
    unittest.main()