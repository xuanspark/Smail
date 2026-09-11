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
    page.wait_for_timeout(3000)

    page.locator('.settings-btn').click()
    page.wait_for_timeout(1500)
    page.locator('#tab-config').click()
    page.wait_for_timeout(2000)

    # List all buttons in .config-navigation
    btns = page.locator('.config-navigation button')
    print('Navigation buttons count:', btns.count())
    for i in range(btns.count()):
        text = btns.nth(i).inner_text().strip().replace(chr(10), ' ')
        print(f'Btn {i}: {text}')

    # Click Email Config
    if btns.count() > 1:
        btns.nth(1).click()
        page.wait_for_timeout(2000)
        page.screenshot(path=os.path.join(test_dir, '03_config_mail.png'))

    # Click SSL Config
    if btns.count() > 2:
        btns.nth(2).click()
        page.wait_for_timeout(2000)
        page.screenshot(path=os.path.join(test_dir, '04_config_ssl.png'))

    # Test Plugins tab in settings
    page.locator('#tab-plugins').click()
    page.wait_for_timeout(2000)
    page.screenshot(path=os.path.join(test_dir, '05_plugins.png'))

    # Test Users tab in settings
    page.locator('#tab-users').click()
    page.wait_for_timeout(2000)
    page.screenshot(path=os.path.join(test_dir, '06_users.png'))

    # Test Logs tab in settings
    page.locator('#tab-logs').click()
    page.wait_for_timeout(2000)
    page.screenshot(path=os.path.join(test_dir, '07_logs.png'))

    # Test Security tab in settings
    page.locator('#tab-security').click()
    page.wait_for_timeout(2000)
    page.screenshot(path=os.path.join(test_dir, '08_security.png'))

    browser.close()
