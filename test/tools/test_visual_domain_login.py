import time, os
from playwright.sync_api import sync_playwright

test_log_dir = os.path.join(os.getcwd(), 'test-log', 'domain-login-acceptance-v2')
os.makedirs(test_log_dir, exist_ok=True)

with sync_playwright() as p:
    browser = p.chromium.launch(
        headless=False,
        slow_mo=600,
        args=['--start-maximized', '--no-sandbox']
    )
    context = browser.new_context(viewport=None)
    page = context.new_page()

    page.on('console', lambda msg: print('[CONSOLE]', msg.type, msg.text))
    page.on('pageerror', lambda exc: print('[ERROR]', exc))
    page.on('request', lambda req: print('[REQ]', req.method, req.url) if 'api' in req.url else None)
    page.on('response', lambda res: print('[RES]', res.status, res.url) if 'api' in res.url else None)

    page.goto('https://mail.example.com/#/login', wait_until='networkidle')
    time.sleep(2)

    acc = page.locator('.login-content input').nth(0)
    pwd = page.locator('.login-content input').nth(1)
    acc.fill('xuans')
    pwd.fill('12345678')
    time.sleep(1)

    page.locator('button.login-btn').click()

    time.sleep(5)
    page.screenshot(path=os.path.join(test_log_dir, 'after_login.png'))
    print('URL after login:', page.url)

    # 检查页面上的文本或状态
    time.sleep(10)
    browser.close()
