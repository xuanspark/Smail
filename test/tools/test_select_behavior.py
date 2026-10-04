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

    # 观察 el-select 结构
    wrapper = page.locator('.border-less-select .el-select__wrapper')
    print('Wrapper count:', wrapper.count())
    wrapper.click()
    page.wait_for_timeout(500)
    
    # 打印当前 active 元素或者向页面输入
    page.keyboard.type('testuser2@example.com')
    page.wait_for_timeout(500)
    page.screenshot(path='test-log/typing_step1.png')

    # 查看下拉菜单是否有内容
    dropdown_visible = page.locator('.el-select-dropdown:visible')
    print('Dropdown visible:', dropdown_visible.count())
    items = page.locator('.el-select-dropdown__item:visible')
    print('Dropdown items count:', items.count())
    for i in range(items.count()):
        print('Item', i, ':', items.nth(i).inner_text())

    # 按 Enter
    page.keyboard.press('Enter')
    page.wait_for_timeout(500)
    page.screenshot(path='test-log/after_enter.png')

    tags = page.locator('.border-less-select .el-tag')
    print('Tags after enter:', tags.count())
    for i in range(tags.count()):
        print('Tag', i, ':', tags.nth(i).inner_text())

    browser.close()
