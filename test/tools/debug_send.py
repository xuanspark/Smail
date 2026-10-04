# -*- coding: utf-8 -*-
import time, os
from playwright.sync_api import sync_playwright

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

    # 填写收件人
    to_input = page.locator('.border-less-select input').first
    to_input.click()
    to_input.fill('testuser2@example.com')
    page.wait_for_timeout(500)
    # 模拟键盘回车和下拉项选择
    page.keyboard.press('Enter')
    page.wait_for_timeout(500)

    # 填写主题
    subject_input = page.locator('.subject-row input')
    subject_input.fill('全量测试主题')

    # 填写正文
    editor = page.locator('.editor-content')
    editor.click()
    page.keyboard.type('测试内容123')
    page.wait_for_timeout(500)

    # 查看当前表单绑定的数据
    tags = page.locator('.el-select__tags-text, .el-tag')
    print('已选收件人标签数量:', tags.count())
    for i in range(tags.count()):
        print('Tag:', tags.nth(i).inner_text())

    # 点击发送
    print('点击发送...')
    page.locator('.send-btn').click()
    page.wait_for_timeout(4000)

    browser.close()
