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

    # 1. 登录 xuans
    print('1. 登录 xuans...')
    page.goto('https://mail.example.com/#/login', wait_until='networkidle')
    inputs = page.locator('.login-content input')
    inputs.nth(0).fill('xuans')
    inputs.nth(1).fill('12345678')
    page.locator('button.login-btn').click()
    page.wait_for_timeout(2500)

    # 2. 检查收件箱中的信任发件人交互
    print('2. 检查收件箱信任操作...')
    page.goto('https://mail.example.com/#/list?tag=0', wait_until='networkidle')
    page.wait_for_timeout(2000)
    
    danger_tags = page.locator('.mail-status-tags .el-tag--danger')
    print('当前收件箱疑似不安全标签数量:', danger_tags.count())
    if danger_tags.count() > 0:
        print('点击第一个不安全标签...')
        danger_tags.first.click()
        page.wait_for_timeout(500)
        page.screenshot(path=os.path.join(output_dir, '30_trust_sender_confirm_box.png'))

        confirm_btn = page.locator('.el-message-box .el-button--primary')
        print('确认按钮数量:', confirm_btn.count())
        confirm_btn.click()
        page.wait_for_timeout(1500)
        page.screenshot(path=os.path.join(output_dir, '31_after_trusted.png'))

    # 3. 检查发件箱标签
    print('3. 检查发件箱...')
    page.goto('https://mail.example.com/#/list?tag=%7B%22type%22%3A1%2C%22status%22%3A-1%7D', wait_until='networkidle')
    page.wait_for_timeout(2000)
    page.screenshot(path=os.path.join(output_dir, '32_outbox_view.png'))

    # 4. 进入写信页，测试删除联系人
    print('4. 写信页测试删除联系人...')
    page.goto('https://mail.example.com/#/editer', wait_until='networkidle')
    page.wait_for_timeout(2000)

    wrapper = page.locator('.border-less-select .el-select__wrapper')
    wrapper.click()
    page.wait_for_timeout(500)

    del_icon = page.locator('.contact-del-icon:visible')
    print('删除图标数量:', del_icon.count())
    if del_icon.count() > 0:
        del_icon.first.click()
        page.wait_for_timeout(1000)
        page.screenshot(path=os.path.join(output_dir, '33_after_delete_contact.png'))

    # 5. 验证多租户隔离：登录 testuser2
    print('5. 验证多租户隔离...')
    page.goto('https://mail.example.com/#/login', wait_until='networkidle')
    inputs = page.locator('.login-content input')
    inputs.nth(0).fill('testuser2')
    inputs.nth(1).fill('12345678')
    page.locator('button.login-btn').click()
    page.wait_for_timeout(2500)

    page.goto('https://mail.example.com/#/editer', wait_until='networkidle')
    page.wait_for_timeout(2000)

    wrapper = page.locator('.border-less-select .el-select__wrapper')
    wrapper.click()
    page.wait_for_timeout(500)
    page.screenshot(path=os.path.join(output_dir, '34_testuser2_contacts_dropdown.png'))
    user2_items = page.locator('.el-select-dropdown__item:visible')
    print('testuser2 联系人下拉选项数:', user2_items.count())

    browser.close()
    print('验收流程全部执行完毕！')
