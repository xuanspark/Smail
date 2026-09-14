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

    html = page.evaluate('''() => {
        const btn = document.querySelector('.refresh-btn');
        return btn ? btn.parentElement.outerHTML : 'none';
    }''')
    print('Parent HTML:\n', html[:500])

    browser.close()
