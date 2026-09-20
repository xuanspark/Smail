"""配置历史实机探针必须真实提交变更，不能以空表存在代替验收。"""

from pathlib import Path
import sys
import unittest
from unittest.mock import MagicMock


sys.path.insert(0, str(Path(__file__).resolve().parents[1] / 'tools'))
from runtime_probe import verify_config_history


class RuntimeConfigTest(unittest.TestCase):
    def client(self, final_generation=2, final_level='warn'):
        client = MagicMock()
        client.success.side_effect = [
            {'available': True, 'activeGeneration': 1},
            {'generation': 1, 'logLevel': 'info', 'databasePasswordStatus': {}},
            {}, {'generation': 2, 'candidateId': 'candidate'}, {},
            {'generation': final_generation, 'logLevel': final_level},
        ]
        return client

    def test_applies_real_change_and_verifies_generation(self):
        client = self.client()
        self.assertTrue(verify_config_history(client)['generation_advanced'])
        calls = client.success.call_args_list
        request = calls[2].args[1]
        self.assertEqual(request['expectedGeneration'], 1)
        self.assertNotIn('databasePasswordStatus', request['payload'])
        self.assertEqual(calls[3].args[2], 'PUT')
        self.assertEqual(calls[4].args[1]['candidateId'], 'candidate')

    def test_rejects_unchanged_generation_or_unapplied_payload(self):
        for generation, level in ((1, 'warn'), (2, 'info')):
            with self.subTest(generation=generation, level=level):
                with self.assertRaises(AssertionError):
                    verify_config_history(self.client(generation, level))


if __name__ == '__main__':
    unittest.main()
