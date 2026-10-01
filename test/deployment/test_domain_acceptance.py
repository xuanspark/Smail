"""验证 Smail 的后台域名、别名跳转和根域名泛域名证书约束。"""

from pathlib import Path
import unittest


ROOT = Path(__file__).resolve().parents[2]
NGINX = (ROOT / 'deploy/nginx/smail.conf').read_text(encoding='utf-8')
CERTIFICATE = (ROOT / 'deploy/nginx/deploy_certificate.py').read_text(encoding='utf-8')
RENEWAL = (ROOT / 'deploy/nginx/renew_certificate.py').read_text(encoding='utf-8')


class DomainAcceptanceTest(unittest.TestCase):
    def test_web_domain_is_the_only_public_web_proxy(self):
        self.assertIn('server_name __SMAIL_WEB_DOMAIN__;', NGINX)
        self.assertIn('proxy_ssl_name __SMAIL_WEB_DOMAIN__;', NGINX)
        self.assertIn('return 308 https://__SMAIL_WEB_DOMAIN__$request_uri;', NGINX)
        self.assertNotIn('return 308 https://__SMAIL_MAIL_DOMAIN__$request_uri;', NGINX)

    def test_certificate_requests_root_and_wildcard(self):
        self.assertIn("DOMAINS = ('cdis.cn', '*.cdis.cn')", CERTIFICATE)
        self.assertIn("CERTIFICATE_SNI = 'web.cdis.cn'", CERTIFICATE)
        self.assertIn("'web.cdis.cn'", CERTIFICATE)
        self.assertIn('DOMAINS = deploy.DOMAINS', RENEWAL)


if __name__ == '__main__':
    unittest.main(verbosity=2)
