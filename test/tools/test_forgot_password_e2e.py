# -*- coding: utf-8 -*-
import os, sys, time, json
from playwright.sync_api import sync_playwright

output_dir = os.path.join(os.getcwd(), 'test-log', '2026-09-14', '截图')
log_file = os.path.join(os.getcwd(), 'test-log', '2026-09-14', '测试log', 'forgot_password_acceptance.log')
os.makedirs(output_dir, exist_ok=True)
os.makedirs(os.path.dirname(log_file), exist_ok=True)

def write_log(msg):
    ts = time.strftime('%Y-%m-%d %H:%M:%S')
    line = f'[{ts}] {msg}'
    print(line)
    with open(log_file, 'a', encoding='utf-8') as f:
        f.write(line + '\n')

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

    write_log('=== 开始忘记密码全流程测试 ===')

    # 1. 登录页检查忘记密码入口
    write_log('1. 打开登录页面')
    page.goto('https://mail.example.com/#/login', wait_until='networkidle')
    page.screenshot(path=os.path.join(output_dir, '10_login_page_forgot_btn.png'))

    forgot_btn = page.locator('button.forgot-password-btn, a:has-text("忘记密码")')
    write_log(f'   忘记密码入口数量: {forgot_btn.count()}')
    if forgot_btn.count() > 0:
        forgot_btn.first.click()
        page.wait_for_timeout(1500)
    else:
        page.goto('https://mail.example.com/#/forgot-password', wait_until='networkidle')
        page.wait_for_timeout(1500)

    write_log(f'   当前页面 URL: {page.url}')
    page.screenshot(path=os.path.join(output_dir, '11_forgot_password_page.png'))

    # 2. 检查页面关键元素与边界提示
    boundary_alert = page.locator('.reset-boundary')
    write_log(f'   超级管理员限制警示框存在: {boundary_alert.count() > 0}')
    if boundary_alert.count() > 0:
        write_log(f'   警示内容: {boundary_alert.inner_text().strip()}')

    # 3. 检查切换验证方式
    switch_btn = page.locator('.switch-button')
    write_log(f'   切换方式按钮文本: {switch_btn.inner_text().strip()}')
    switch_btn.click()
    page.wait_for_timeout(500)
    write_log(f'   切换后按钮文本: {switch_btn.inner_text().strip()}')
    page.screenshot(path=os.path.join(output_dir, '12_switched_to_offline.png'))

    # 4. 测试以超级管理员账号 xuans 进行离线恢复（验证安全拒绝边界）
    write_log('2. 测试超级管理员账号 xuans 在 Web 端重置（必须被安全拒绝）')
    user_input = page.locator('.recovery-form input').nth(0)
    user_input.fill('xuans')
    
    # 离线输入：密保问题、密保答案、恢复码、新密码、确认密码
    q_input = page.locator('.recovery-form input').nth(1)
    a_input = page.locator('.recovery-form input').nth(2)
    code_input = page.locator('.recovery-form input').nth(3)
    p1_input = page.locator('.recovery-form input').nth(4)
    p2_input = page.locator('.recovery-form input').nth(5)

    q_input.fill('幸运数字')
    a_input.fill('8')
    code_input.fill('REC-1234-5678')
    p1_input.fill('NewPass1234!')
    p2_input.fill('NewPass1234!')

    submit_btn = page.locator('.recovery-footer button.recovery-action-btn')
    submit_btn.click()
    page.wait_for_timeout(2000)
    page.screenshot(path=os.path.join(output_dir, '13_superadmin_denied.png'))

    state_span = page.locator('.verification-state')
    msg_box = page.locator('.el-message')
    write_log(f'   状态文本: {state_span.inner_text() if state_span.count() > 0 else "无"}')
    for i in range(msg_box.count()):
        write_log(f'   提示消息: {msg_box.nth(i).inner_text().strip()}')

    browser.close()
