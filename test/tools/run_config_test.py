# -*- coding: utf-8 -*-
import time, os
from playwright.sync_api import sync_playwright

test_dir = os.path.join(os.getcwd(), 'test-log', 'config_verification_suite')
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

    page.locator('.settings-btn').click()
    page.wait_for_timeout(1500)
    page.screenshot(path=os.path.join(test_dir, '01_settings_drawer.png'))

    items = page.locator('.el-tabs__item')
    print('Tabs count:', items.count())
    for i in range(items.count()):
        text = items.nth(i).inner_text().replace(chr(10), ' ')
        print(f'Tab {i}: {text}')

    config_tab = page.locator('#tab-config')
    print('Config tab count:', config_tab.count())
    if config_tab.count() > 0:
        config_tab.click()
        page.wait_for_timeout(2000)
        page.screenshot(path=os.path.join(test_dir, '02_config_base.png'))

    browser.close()
