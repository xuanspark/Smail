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

    page.goto('https://mail.example.com/#/login', wait_until='networkidle')
    inputs = page.locator('.login-content input')
    inputs.nth(0).fill('xuans')
    inputs.nth(1).fill('12345678')
    page.locator('button.login-btn').click()
    page.wait_for_timeout(3000)

    # 打开设置抽屉
    page.locator('.settings-btn').click()
    page.wait_for_timeout(1500)
    page.locator('#tab-users').click()
    page.wait_for_timeout(2000)
    page.screenshot(path=os.path.join(test_dir, '01_users_table.png'))

    # 打印表格表头和行数据
    rows = page.locator('.modern-table .el-table__row')
    print('用户行数:', rows.count())
    for i in range(rows.count()):
        print(f'Row {i}:', rows.nth(i).inner_text().replace('\n', ' | '))

    # 点击“新建”按钮
    new_btn = page.locator('.new-btn, button:has-text("新建")')
    print('新建按钮数量:', new_btn.count())
    if new_btn.count() > 0:
        new_btn.first.click()
        page.wait_for_timeout(1000)
        page.screenshot(path=os.path.join(test_dir, '02_create_user_dialog.png'))

        # 填写新用户信息
        dialog = page.locator('.el-dialog:visible')
        d_inputs = dialog.locator('input')
        print('对话框输入框数量:', d_inputs.count())
        # 0: account, 1: name, 2: password
        if d_inputs.count() >= 3:
            d_inputs.nth(0).fill('tester01')
            d_inputs.nth(1).fill('测试员')
            d_inputs.nth(2).fill('Tester123456!')
            page.wait_for_timeout(500)
            page.screenshot(path=os.path.join(test_dir, '03_form_filled.png'))

            # 点击确认按钮
            confirm_btn = dialog.locator('button.el-button--primary:has-text("确定"), button.el-button--primary:has-text("确认")')
            print('确认按钮数量:', confirm_btn.count())
            if confirm_btn.count() > 0:
                confirm_btn.last.click()
                page.wait_for_timeout(2500)
                page.screenshot(path=os.path.join(test_dir, '04_after_user_created.png'))

    browser.close()
