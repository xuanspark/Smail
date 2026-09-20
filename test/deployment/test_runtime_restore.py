"""冷备归档与协议正文校验反例，不启动任何真实服务。"""

from email.message import EmailMessage
from io import BytesIO
from pathlib import Path
import sys
import tarfile
import tempfile
import unittest

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / 'tools'))
from runtime_probe import verify_message_content
from runtime_restore import archive_members, tree_digest


class RuntimeRestoreTest(unittest.TestCase):
    def test_archive_rejects_traversal_links_and_devices(self):
        for name, kind in [('instance/../outside', tarfile.REGTYPE),
                           ('/instance/file', tarfile.REGTYPE),
                           ('other/file', tarfile.REGTYPE),
                           ('instance/link', tarfile.SYMTYPE),
                           ('instance/hard', tarfile.LNKTYPE),
                           ('instance/device', tarfile.CHRTYPE)]:
            with self.subTest(name=name), tarfile.open(fileobj=BytesIO(), mode='w') as archive:
                member = tarfile.TarInfo(name)
                member.type = kind
                archive.addfile(member)
                with self.assertRaises(AssertionError):
                    archive_members(archive)

    def test_archive_accepts_only_regular_instance_content(self):
        with tarfile.open(fileobj=BytesIO(), mode='w') as archive:
            member = tarfile.TarInfo('instance')
            member.type = tarfile.DIRTYPE
            archive.addfile(member)
            archive.addfile(tarfile.TarInfo('instance/smail.db'))
            self.assertEqual(len(archive_members(archive)), 2)

    def test_digest_detects_modified_backup_bytes(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            path = root / 'smail.db'
            path.write_bytes(b'original')
            before = tree_digest(root)
            path.write_bytes(b'changed')
            self.assertNotEqual(tree_digest(root), before)

    def test_mime_checks_body_filename_message_and_attachment_bytes(self):
        message = EmailMessage()
        message['Message-ID'] = '<acceptance@runtime.invalid>'
        message.set_content('中文验收正文')
        message.add_attachment(b'payload', maintype='text', subtype='plain', filename='验收附件.txt')
        raw = message.as_bytes()
        verify_message_content(raw, 'acceptance@runtime.invalid', '中文验收正文', b'payload')
        for identifier, body, attachment in [('wrong', '中文验收正文', b'payload'),
                                              ('acceptance@runtime.invalid', '缺失正文', b'payload'),
                                              ('acceptance@runtime.invalid', '中文验收正文', b'corrupted')]:
            with self.subTest(identifier=identifier, body=body), self.assertRaises(AssertionError):
                verify_message_content(raw, identifier, body, attachment)


if __name__ == '__main__':
    unittest.main()
