# -*- coding: utf-8 -*-
import time, os
from playwright.sync_api import sync_playwright

test_dir = os.path.join(os.getcwd(), 'test-log', 'mail_flow_suite')
os.makedirs(test_dir, exist_ok=True)

with sync_playwright() as p:
    browser = p.chromium.launch(
        headless=True,
        args=[
            '--no-sandbox',
            '--ignore-certificate-errors',
            '--host-resolver-rules=MAP mail.solit.cn 47.108.78.12, MAP solit.cn 47.108.78.12'
        ]
    )
    context = browser.new_context(viewport={'width': 1440, 'height': 900}, ignore_https_errors=True)
    page = context.new_page()

    page.on('response', lambda res: print('[API]', res.status, res.url, res.text()[:250]) if 'email' in res.url or 'group' in res.url else None)

    page.goto('https://mail.solit.cn/#/login', wait_until='networkidle')
    inputs = page.locator('.login-content input')
    inputs.nth(0).fill('xuans')
    inputs.nth(1).fill('12345678')
    page.locator('button.login-btn').click()
    page.wait_for_timeout(3000)

    # 打印菜单项
    menu_items = page.locator('.sidebar-menu .el-menu-item')
    print('侧边栏菜单项数:', menu_items.count())
    for i in range(menu_items.count()):
        print(f'Menu {i}:', menu_items.nth(i).inner_text().strip())

    # 点击发件箱 / 已发送
    if menu_items.count() > 1:
        menu_items.nth(1).click()
        page.wait_for_timeout(2000)
        page.screenshot(path=os.path.join(test_dir, '25_outbox_clicked.png'))

    browser.close()
