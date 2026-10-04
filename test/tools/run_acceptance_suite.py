# -*- coding: utf-8 -*-
import os, sys
from playwright.sync_api import sync_playwright

output_dir = os.path.join(os.getcwd(), 'test-log', 'acceptance_contact_security')
os.makedirs(output_dir, exist_ok=True)

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

    # 1. 登录
    print('--- Step 1: Login ---')
    page.goto('https://mail.example.com/#/login', wait_until='networkidle')
    inputs = page.locator('.login-content input')
    inputs.nth(0).fill('xuans')
    inputs.nth(1).fill('12345678')
    page.locator('button.login-btn').click()
    page.wait_for_timeout(2500)
    page.screenshot(path=os.path.join(output_dir, '01_logged_in.png'))
    print('Logged in, URL:', page.url)

    # 2. 验证收件箱安全提示
    print('--- Step 2: Inbox Security Tags ---')
    page.goto('https://mail.example.com/#/list?tag=0', wait_until='networkidle')
    page.wait_for_timeout(2000)
    page.screenshot(path=os.path.join(output_dir, '02_inbox_list.png'))

    danger_tags = page.locator('.mail-status-tags .el-tag--danger')
    print('Inbox danger tag count:', danger_tags.count())
    for i in range(danger_tags.count()):
        print('Danger tag text:', danger_tags.nth(i).inner_text())

    # 3. 验证发件箱状态标签与错误详情
    print('--- Step 3: Outbox Status Tags & Error Dialog ---')
    page.goto('https://mail.example.com/#/list?tag=1', wait_until='networkidle')
    page.wait_for_timeout(2000)
    page.screenshot(path=os.path.join(output_dir, '03_outbox_list.png'))
    success_tags = page.locator('.mail-status-tags .el-tag--success')
    fail_tags = page.locator('.mail-status-tags .el-tag--danger')
    print('Outbox success tag count:', success_tags.count())
    print('Outbox fail tag count:', fail_tags.count())

    if fail_tags.count() > 0:
        print('Clicking fail tag...')
        fail_tags.first.click()
        page.wait_for_timeout(1000)
        page.screenshot(path=os.path.join(output_dir, '04_outbox_error_dialog.png'))
        close_btn = page.locator('.el-dialog button')
        if close_btn.count() > 0:
            close_btn.first.click()
            page.wait_for_timeout(500)

    # 4. 验证写信页联系人
    print('--- Step 4: Editer Page Contacts ---')
    page.goto('https://mail.example.com/#/editer', wait_until='networkidle')
    page.wait_for_timeout(2000)
    page.screenshot(path=os.path.join(output_dir, '05_editer_page.png'))

    select_input = page.locator('.border-less-select')
    select_input.click()
    page.wait_for_timeout(800)
    page.screenshot(path=os.path.join(output_dir, '06_editer_contacts_dropdown.png'))

    dropdown_items = page.locator('.el-select-dropdown__item')
    print('Dropdown items count:', dropdown_items.count())
    for i in range(dropdown_items.count()):
        print('Item:', dropdown_items.nth(i).inner_text())

    browser.close()
    print('Acceptance check finished.')
