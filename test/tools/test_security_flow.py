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
    page.screenshot(path=os.path.join(test_dir, '01_security_tab.png'))

    # 查找安全中心内所有的按钮或卡片操作
    btns = page.locator('.settings-drawer button')
    print('Buttons in settings drawer:', btns.count())
    for i in range(btns.count()):
        t = btns.nth(i).inner_text().strip().replace('\n', ' ')
        if t:
            print(f'Btn {i}: {t}')

    # 寻找设置密保按钮
    set_q_btn = page.locator('button:has-text("密保")')
    print('密保按钮数量:', set_q_btn.count())
    if set_q_btn.count() > 0:
        set_q_btn.first.click()
        page.wait_for_timeout(1500)
        page.screenshot(path=os.path.join(test_dir, '02_question_dialog.png'))

        dialog = page.locator('.el-dialog')
        d_inputs = dialog.locator('input')
        print('弹窗内输入框数量:', d_inputs.count())
        if d_inputs.count() >= 2:
            d_inputs.nth(0).fill('幸运数字')
            d_inputs.nth(1).fill('8')
            page.wait_for_timeout(500)
            page.screenshot(path=os.path.join(test_dir, '03_question_filled.png'))

            confirm_btn = dialog.locator('button:has-text("确定"), button:has-text("保存")')
            print('确定按钮数量:', confirm_btn.count())
            if confirm_btn.count() > 0:
                confirm_btn.last.click()
                page.wait_for_timeout(2500)
                page.screenshot(path=os.path.join(test_dir, '04_after_question_save.png'))

    browser.close()
