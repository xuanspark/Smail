# -*- coding: utf-8 -*-
import os, sys
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

    page.goto('https://mail.example.com/#/login', wait_until='networkidle')
    inputs = page.locator('.login-content input')
    inputs.nth(0).fill('xuans')
    inputs.nth(1).fill('12345678')
    page.locator('button.login-btn').click()
    page.wait_for_timeout(2500)

    page.goto('https://mail.example.com/#/editer', wait_until='networkidle')
    page.wait_for_timeout(2000)

    # 打印页面上的 sender 相关信息
    sender_text = page.locator('.sender-selector').inner_text()
    print('Sender text:', repr(sender_text))

    # 尝试用正确的选择器选 input
    input_elem = page.locator('.border-less-select input').first
    input_elem.click()
    page.keyboard.type('testuser2@example.com')
    page.wait_for_timeout(500)
    page.keyboard.press('Enter')
    page.wait_for_timeout(500)

    tags = page.locator('.border-less-select .el-tag')
    print('Tags after typing & Enter:', tags.count())
    if tags.count() > 0:
        print('Tag content:', tags.first.inner_text())

    # 填写主题
    page.locator('.subject-row input').fill('自动化验证测试邮件')

    # 填写正文
    page.locator('.editor-content').click()
    page.keyboard.type('正文内容...')
    page.wait_for_timeout(500)

    page.locator('.send-btn').click()
    page.wait_for_timeout(1000)

    errors = page.locator('.el-form-item__error')
    print('Form errors count:', errors.count())
    for i in range(errors.count()):
        print('Error', i, ':', errors.nth(i).inner_text())

    browser.close()
