# -*- coding: utf-8 -*-
import time, os
from playwright.sync_api import sync_playwright

test_dir = os.path.join(os.getcwd(), 'test-log', 'user_management_suite')
os.makedirs(test_dir, exist_ok=True)

with sync_playwright() as p:
    browser = p.chromium.launch(
        headless=True,
        args=[
            '--no-sandbox',
            '--ignore-certificate-errors',
            '--host-resolver-rules=MAP mail.example.com 47.108.78.12, MAP example.com 47.108.78.12'
        ]
    )
    context = browser.new_context(viewport={'width': 1440, 'height': 900}, ignore_https_errors=True)
    page = context.new_page()

    page.on('response', lambda res: print('[API]', res.status, res.url, res.text()[:200]) if 'user' in res.url else None)

    page.goto('https://mail.example.com/#/login', wait_until='networkidle')
    inputs = page.locator('.login-content input')
    inputs.nth(0).fill('xuans')
    inputs.nth(1).fill('12345678')
    page.locator('button.login-btn').click()
    page.wait_for_timeout(3000)

    page.locator('.settings-btn').click()
    page.wait_for_timeout(1500)
    page.locator('#tab-users').click()
    page.wait_for_timeout(2000)

    # 再次新建一个用户测试响应
    page.locator('.new-btn').click()
    page.wait_for_timeout(1000)
    dialog = page.locator('.el-dialog:visible')
    d_inputs = dialog.locator('input')
    d_inputs.nth(0).fill('testuser2')
    d_inputs.nth(1).fill('测试员二')
    d_inputs.nth(2).fill('Test123456')
    page.wait_for_timeout(500)
    dialog.locator('button.el-button--primary:has-text("确定"), button.el-button--primary:has-text("确认")').click()
    page.wait_for_timeout(3000)

    # 查看当前表格
    rows = page.locator('.modern-table .el-table__row')
    print('最新用户列表总行数:', rows.count())
    for i in range(rows.count()):
        print(f'Row {i}:', rows.nth(i).inner_text().replace('\n', ' | '))

    browser.close()
