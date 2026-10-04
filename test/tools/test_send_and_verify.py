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
            '--host-resolver-rules=MAP mail.example.com 47.108.78.12, MAP example.com 47.108.78.12'
        ]
    )
    context = browser.new_context(viewport={'width': 1440, 'height': 900}, ignore_https_errors=True)
    page = context.new_page()

    page.on('response', lambda res: print('[API RES]', res.status, res.url, res.text()[:200]) if 'email' in res.url or 'send' in res.url else None)

    page.goto('https://mail.example.com/#/login', wait_until='networkidle')
    inputs = page.locator('.login-content input')
    inputs.nth(0).fill('xuans')
    inputs.nth(1).fill('12345678')
    page.locator('button.login-btn').click()
    page.wait_for_timeout(3000)

    page.goto('https://mail.example.com/#/editer', wait_until='networkidle')
    page.wait_for_timeout(2000)

    # 聚焦到收件人输入框
    wrapper = page.locator('.border-less-select .el-select__wrapper').first
    wrapper.click()
    page.wait_for_timeout(300)

    to_input = page.locator('.border-less-select input').first
    to_input.fill('testuser2@example.com')
    page.wait_for_timeout(300)
    page.keyboard.press('Enter')
    page.wait_for_timeout(500)

    # 填写主题
    subject_input = page.locator('.subject-row input')
    subject_input.fill('全量测试邮件主题 - 自动化验证')

    # 填写正文
    editor = page.locator('.editor-content')
    editor.click()
    page.keyboard.type('测试内容：这是由全量验收流程发送的正式测试邮件。')
    page.wait_for_timeout(500)
    page.screenshot(path=os.path.join(test_dir, '10_mail_composed.png'))

    # 查看当前已选收件人标签
    tags = page.locator('.border-less-select .el-tag')
    print('已选收件人标签数量:', tags.count())
    for i in range(tags.count()):
        print('Tag:', tags.nth(i).inner_text())

    # 点击发送按钮
    print('点击发送邮件...')
    page.locator('.send-btn').click()
    page.wait_for_timeout(5000)
    page.screenshot(path=os.path.join(test_dir, '11_after_send.png'))
    print('发送后当前页面 URL:', page.url)

    # 检查已发送列表
    page.goto('https://mail.example.com/#/list?tag=outbox', wait_until='networkidle')
    page.wait_for_timeout(2000)
    page.screenshot(path=os.path.join(test_dir, '12_outbox_list.png'))

    # 查看列表中的邮件
    items = page.locator('.email-item, .mail-item, tr.el-table__row')
    print('已发送列表条目数:', items.count())
    for i in range(min(5, items.count())):
        print(f'Mail {i}:', items.nth(i).inner_text().replace('\n', ' | '))

    browser.close()
