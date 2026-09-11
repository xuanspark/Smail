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

    page.goto('https://mail.solit.cn/#/login', wait_until='networkidle')
    inputs = page.locator('.login-content input')
    inputs.nth(0).fill('xuans')
    inputs.nth(1).fill('12345678')
    page.locator('button.login-btn').click()
    page.wait_for_timeout(3000)

    # 打开设置抽屉并进入日志
    page.locator('.settings-btn').click()
    page.wait_for_timeout(1500)
    page.locator('#tab-logs').click()
    page.wait_for_timeout(2000)

    # 查看三大单选按钮
    radio_btns = page.locator('.log-categories .el-radio-button')
    print('单选按钮数量:', radio_btns.count())
    for i in range(radio_btns.count()):
        print(f'Radio {i}:', radio_btns.nth(i).inner_text())

    # 1. 点击“运行日志”
    run_btn = page.locator('.log-categories .el-radio-button:has-text("运行日志")')
    if run_btn.count() > 0:
        run_btn.click()
        page.wait_for_timeout(2000)
        page.screenshot(path=os.path.join(test_dir, '10_runtime_logs.png'))

        # 点击第一条运行日志的“查看”按钮
        view_btn = page.locator('.runtime-table .el-button--primary').first
        if view_btn.count() > 0:
            view_btn.click()
            page.wait_for_timeout(1000)
            page.screenshot(path=os.path.join(test_dir, '11_runtime_detail_dialog.png'))
            # 关闭弹窗
            page.keyboard.press('Escape')
            page.wait_for_timeout(500)

    # 2. 点击“收发信日志”
    mail_btn = page.locator('.log-categories .el-radio-button:has-text("收发")')
    if mail_btn.count() > 0:
        mail_btn.click()
        page.wait_for_timeout(2000)
        page.screenshot(path=os.path.join(test_dir, '20_mail_logs.png'))

        # 检查刚刚发送邮件的收发信日志
        rows = page.locator('.log-table .el-table__row')
        print('收发信日志行数:', rows.count())
        for i in range(min(5, rows.count())):
            print(f'Mail Log {i}:', rows.nth(i).inner_text().replace('\n', ' | '))

    browser.close()
