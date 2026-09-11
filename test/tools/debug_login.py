import time, os
from playwright.sync_api import sync_playwright

test_log_dir = os.path.join(os.getcwd(), 'test-log', 'debug-login')
os.makedirs(test_log_dir, exist_ok=True)

with sync_playwright() as p:
    browser = p.chromium.launch(headless=False, slow_mo=600, args=['--start-maximized', '--ignore-certificate-errors'])
    context = browser.new_context(viewport=None, ignore_https_errors=True)
    page = context.new_page()

    page.on('console', lambda msg: print('[CONSOLE]', msg.type, msg.text))
    page.on('pageerror', lambda exc: print('[PAGE ERROR]', exc))

    def on_res(res):
        try:
            print('[RES]', res.status, res.url, res.text()[:150])
        except Exception:
            print('[RES]', res.status, res.url)
    page.on('response', on_res)

    page.goto('https://47.108.78.12/#/login', wait_until='networkidle')
    time.sleep(2)

    acc = page.locator('.login-content input').nth(0)
    pwd = page.locator('.login-content input').nth(1)
    acc.fill('xuans')
    pwd.fill('12345678')
    time.sleep(1)

    btn = page.locator('button.login-btn')
    btn.click()

    time.sleep(4)
    # 检查所有 el-message
    messages = page.locator('.el-message')
    print('El-Messages count:', messages.count())
    for i in range(messages.count()):
        print(f'Msg {i}: {messages.nth(i).inner_text()}')

    page.screenshot(path=os.path.join(test_log_dir, '04_detailed.png'))
    time.sleep(3)
    browser.close()
