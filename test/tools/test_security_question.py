# -*- coding: utf-8 -*-
import time, os
from playwright.sync_api import sync_playwright

test_dir = os.path.join(os.getcwd(), 'test-log', 'security_verification_suite')
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
    page.locator('#tab-security').click()
    page.wait_for_timeout(2000)

    # 点击左侧子菜单中的“密保问题”
    question_nav = page.locator('.security-workspace .settings-submenu-item:has-text("密保"), .security-workspace button:has-text("密保")')
    print('密保菜单数量:', question_nav.count())
    if question_nav.count() > 0:
        question_nav.first.click()
        page.wait_for_timeout(1500)
        page.screenshot(path=os.path.join(test_dir, '10_security_question_section.png'))

        # 在表单中输入密保问题和 1 位密保答案
        inputs = page.locator('.security-main input')
        print('安全主体输入框数量:', inputs.count())
        if inputs.count() >= 2:
            inputs.nth(0).fill('幸运数字')
            inputs.nth(1).fill('8')
            page.wait_for_timeout(500)
            page.screenshot(path=os.path.join(test_dir, '11_question_filled_1char.png'))

            # 点击保存/更新密保问题按钮
            save_btn = page.locator('.security-main button.el-button--primary')
            print('保存按钮数量:', save_btn.count())
            if save_btn.count() > 0:
                save_btn.first.click()
                page.wait_for_timeout(2500)
                page.screenshot(path=os.path.join(test_dir, '12_question_saved_result.png'))

    browser.close()
