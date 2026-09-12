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

    page.goto('https://mail.solit.cn/#/login', wait_until='networkidle')
    inputs = page.locator('.login-content input')
    inputs.nth(0).fill('xuans')
    inputs.nth(1).fill('12345678')
    page.locator('button.login-btn').click()
    page.wait_for_timeout(2500)

    page.goto('https://mail.solit.cn/#/editer', wait_until='networkidle')
    page.wait_for_timeout(2000)

    # 展开下拉列表
    wrapper = page.locator('.border-less-select .el-select__wrapper')
    wrapper.click()
    page.wait_for_timeout(500)

    items = page.locator('.el-select-dropdown__item:visible')
    print('Dropdown items count:', items.count())
    for i in range(items.count()):
        print('Item', i, ':', items.nth(i).inner_text())

    # 截图
    page.screenshot(path='test-log/contacts_dropdown_rendered.png')

    browser.close()
