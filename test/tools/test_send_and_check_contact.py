# -*- coding: utf-8 -*-
import os, sys, time
from playwright.sync_api import sync_playwright

output_dir = os.path.join(os.getcwd(), 'test-log', 'acceptance_contact_security')
os.makedirs(output_dir, exist_ok=True)

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

    # 登录
    print('1. 登录 xuans...')
    page.goto('https://mail.solit.cn/#/login', wait_until='networkidle')
    inputs = page.locator('.login-content input')
    inputs.nth(0).fill('xuans')
    inputs.nth(1).fill('12345678')
    page.locator('button.login-btn').click()
    page.wait_for_timeout(2500)

    # 打开写信页
    print('2. 前往写信页...')
    page.goto('https://mail.solit.cn/#/editer', wait_until='networkidle')
    page.wait_for_timeout(2000)

    # 填写收件人
    select_box = page.locator('.border-less-select')
    select_box.click()
    page.wait_for_timeout(300)
    page.keyboard.type('testuser2@solit.cn')
    page.wait_for_timeout(500)
    page.keyboard.press('Enter')
    page.wait_for_timeout(500)

    # 填写主题
    page.locator('.subject-row input').fill('自动化联系人验证测试')
    # 填写正文
    page.locator('.editor-content').click()
    page.keyboard.type('测试正文：验证发信后收件人是否记录入库。')
    page.wait_for_timeout(500)

    print('3. 点击发送...')
    page.locator('.send-btn').click()
    page.wait_for_timeout(4000)
    page.screenshot(path=os.path.join(output_dir, '10_sent_first_mail.png'))

    # 再次前往写信页，查看下拉列表
    print('4. 重新进入写信页，查看历史联系人下拉...')
    page.goto('https://mail.solit.cn/#/editer', wait_until='networkidle')
    page.wait_for_timeout(2000)

    select_box = page.locator('.border-less-select')
    select_box.click()
    page.wait_for_timeout(800)
    page.screenshot(path=os.path.join(output_dir, '11_contacts_dropdown_after_send.png'))

    items = page.locator('.el-select-dropdown__item:visible')
    print('下拉数量:', items.count())
    for i in range(items.count()):
        print('Item', i, ':', items.nth(i).inner_text())

    browser.close()
