# -*- coding: utf-8 -*-
import os, sys
from playwright.sync_api import sync_playwright

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

    page.on('console', lambda msg: print('[CONSOLE]', msg.type, msg.text))
    page.on('pageerror', lambda err: print('[PAGE ERROR]', err))
    page.on('response', lambda res: print('[NET RES]', res.status, res.url) if '/api/' in res.url else None)

    page.goto('https://mail.solit.cn/#/login', wait_until='networkidle')
    inputs = page.locator('.login-content input')
    inputs.nth(0).fill('xuans')
    inputs.nth(1).fill('12345678')
    page.locator('button.login-btn').click()
    page.wait_for_timeout(2500)

    page.goto('https://mail.solit.cn/#/editer', wait_until='networkidle')
    page.wait_for_timeout(2000)

    # 选择收件人
    wrapper = page.locator('.border-less-select .el-select__wrapper')
    wrapper.click()
    page.wait_for_timeout(300)
    page.keyboard.type('testuser2@solit.cn')
    page.wait_for_timeout(500)
    
    item = page.locator('.el-select-dropdown__item:visible').first
    item.click()
    page.wait_for_timeout(500)

    tags = page.locator('.border-less-select .el-tag')
    print('Tags count:', tags.count())

    # 填写主题
    page.locator('.subject-row input').fill('自动化测试验证收件人')

    # 填写正文
    page.locator('.editor-content').click()
    page.keyboard.type('测试正文内容123456')
    page.wait_for_timeout(500)

    print('Clicking send...')
    page.locator('.send-btn').click()
    page.wait_for_timeout(4000)

    print('After click send, URL:', page.url)
    errors = page.locator('.el-form-item__error')
    print('Errors count:', errors.count())
    for i in range(errors.count()):
        print('Error text:', errors.nth(i).inner_text())

    browser.close()
