# -*- coding: utf-8 -*-
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
    page.wait_for_timeout(3000)

    print('Current URL:', page.url)
    all_btns = page.locator('button')
    print('Total buttons:', all_btns.count())
    for i in range(all_btns.count()):
        btn = all_btns.nth(i)
        print(f'Button {i}: class="{btn.get_attribute("class")}", text="{btn.inner_text().strip()}"')

    browser.close()
