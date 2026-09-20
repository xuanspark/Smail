"""旧连接撤销探针必须区分新登录拒绝与存量连接失效。"""

import imaplib
import poplib
import smtplib
import sys
import unittest
from pathlib import Path
from unittest.mock import MagicMock, patch

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / 'tools'))
import runtime_probe


class RevocationProbeTests(unittest.TestCase):
    def run_probe(self, revoked, custom=False):
        smtp, new_smtp = MagicMock(), MagicMock()
        imap, new_imap = MagicMock(), MagicMock()
        pop, new_pop = MagicMock(), MagicMock()
        client = MagicMock()
        client.success.return_value = {'account': runtime_probe.ACCOUNT}
        smtp.auth.return_value = (235, b'OK')
        smtp.mail.side_effect = [(250, b'OK'), (535 if revoked else 250, b'result')]
        smtp.rcpt.return_value = smtp.rset.return_value = (250, b'OK')
        imap.login.return_value = imap.select.return_value = ('OK', [])
        imap.uid.side_effect = [('OK', [(b'1', b'mail')]), ('NO' if revoked else 'OK', [])]
        pop.list.return_value = (b'+OK', [b'1 123'], 0)
        pop.retr.side_effect = [(b'+OK', [], 0), poplib.error_proto(b'-ERR') if revoked else (b'+OK', [], 0)]
        new_smtp.auth.side_effect = smtplib.SMTPAuthenticationError(535, b'denied')
        new_imap.login.side_effect = imaplib.IMAP4.error('AUTHENTICATIONFAILED')
        new_pop.pass_.side_effect = poplib.error_proto(b'-ERR')
        revoke = MagicMock() if custom else None
        with patch.object(runtime_probe, 'smtp_connect', side_effect=[smtp, new_smtp]), \
             patch.object(runtime_probe, 'LocalIMAP', side_effect=[imap, new_imap]), \
             patch.object(runtime_probe, 'LocalPOP', side_effect=[pop, new_pop]):
            result = runtime_probe.verify_protocol_revocation(client, None, dict.fromkeys(('smtp', 'imap', 'pop3'), 'test-code'), revoke)
        if custom:
            revoke.assert_called_once()
            self.assertEqual(client.success.call_count, 1)
        for protocol in ('smtp', 'imap', 'pop3'):
            self.assertTrue(result[protocol + '_new_login_denied'])
            self.assertEqual(result[protocol + '_existing_denied'], revoked)
        smtp.close.assert_called_once()
        imap.shutdown.assert_called_once()
        pop.close.assert_called_once()

    def test_new_login_rejection_does_not_hide_old_connection_access(self):
        self.run_probe(False)

    def test_old_and_new_connections_both_rejected(self):
        self.run_probe(True)

    def test_client_revocation_uses_supplied_action(self):
        self.run_probe(True, custom=True)

    def test_partial_connect_failure_closes_prior_connections(self):
        smtp = MagicMock()
        with patch.object(runtime_probe, 'smtp_connect', return_value=smtp), \
             patch.object(runtime_probe, 'LocalIMAP', side_effect=OSError('connection failed')):
            with self.assertRaises(OSError):
                runtime_probe.verify_protocol_revocation(MagicMock(), None, {})
        smtp.close.assert_called_once()


if __name__ == '__main__':
    unittest.main()
