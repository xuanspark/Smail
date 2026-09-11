# -*- coding: utf-8 -*-
import time, os
from playwright.sync_api import sync_playwright

test_dir = os.path.join(os.getcwd(), 'test-log', 'log_audit_suite')
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

    page.on('response', lambda res: print('[API]', res.status, res.url) if 'log' in res.url or 'audit' in res.url else None)

    page.goto('https://mail.solit.cn/#/login', wait_until='networkidle')
    inputs = page.locator('.login-content input')
    inputs.nth(0).fill('xuans')
    inputs.nth(1).fill('12345678')
    page.locator('button.login-btn').click()
    page.wait_for_timeout(3000)

    # 打开设置抽屉
    page.locator('.settings-btn').click()
    page.wait_for_timeout(1500)
    page.locator('#tab-logs').click()
    page.wait_for_timeout(2000)
    page.screenshot(path=os.path.join(test_dir, '01_logs_overview.png'))

    # 查看日志页面中的子菜单/选项卡
    log_tabs = page.locator('.log-settings button, .settings-submenu-item')
    print('日志导航选项数量:', log_tabs.count())
    for i in range(log_tabs.count()):
        t = log_tabs.nth(i).inner_text().strip().replace('\n', ' ')
        print(f'Tab {i}: {t}')

    # 查看运行日志表格
    rows = page.locator('.el-table__row')
    print('当前日志行数:', rows.count())
    for i in range(min(5, rows.count())):
        print(f'Row {i}:', rows.nth(i).inner_text().replace('\n', ' | '))

    # 切换子菜单查看操作日志和收发信日志（如果有）
    if log_tabs.count() > 1:
        log_tabs.nth(1).click()
        page.wait_for_timeout(2000)
        page.screenshot(path=os.path.join(test_dir, '02_log_subtab1.png'))
    if log_tabs.count() > 2:
        log_tabs.nth(2).click()
        page.wait_for_timeout(2000)
        page.screenshot(path=os.path.join(test_dir, '03_log_subtab2.png'))

    browser.close()
