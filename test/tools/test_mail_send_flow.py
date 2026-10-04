# -*- coding: utf-8 -*-
import time, os
from playwright.sync_api import sync_playwright

test_dir = os.path.join(os.getcwd(), 'test-log', 'mail_flow_suite')
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

    page.on('response', lambda res: print('[API]', res.status, res.url) if 'api' in res.url else None)

    page.goto('https://mail.example.com/#/login', wait_until='networkidle')
    inputs = page.locator('.login-content input')
    inputs.nth(0).fill('xuans')
    inputs.nth(1).fill('12345678')
    page.locator('button.login-btn').click()
    page.wait_for_timeout(3000)

    # 查看侧边栏并点击“写邮件”或路由切换到 /editer
    page.goto('https://mail.example.com/#/editer', wait_until='networkidle')
    page.wait_for_timeout(2000)
    page.screenshot(path=os.path.join(test_dir, '01_editer_page.png'))

    # 查看发件人信息
    sender_info = page.locator('.sender-selector').inner_text()
    print('当前发件人展示:', sender_info)

    # 填写收件人
    # 收件人是一个 el-select 允许输入的
    to_input = page.locator('.border-less-select input').first
    to_input.fill('testuser2@example.com')
    page.keyboard.press('Enter')
    page.wait_for_timeout(500)

    # 填写主题
    subject_input = page.locator('.subject-row input')
    subject_input.fill('全量测试验收邮件 - ' + str(int(time.time())))

    # 在富文本编辑器中输入正文
    editor = page.locator('.editor-content')
    editor.click()
    page.keyboard.type('这是一封用于全量功能与发件箱归档测试的真实邮件内容。')
    page.wait_for_timeout(500)
    page.screenshot(path=os.path.join(test_dir, '02_mail_filled.png'))

    # 点击发送
    print('点击发送邮件...')
    page.locator('.send-btn').click()
    page.wait_for_timeout(4000)
    page.screenshot(path=os.path.join(test_dir, '03_after_send.png'))
    print('当前页面 URL:', page.url)

    # 查看发件箱
    print('切换至发件箱...')
    # 查找侧边栏中名为“已发送”或“发件箱”的菜单项
    outbox_item = page.locator('.sidebar-menu .el-menu-item:has-text("已发送"), .sidebar-menu .el-menu-item:has-text("发件箱")')
    print('发件箱菜单数量:', outbox_item.count())
    if outbox_item.count() > 0:
        outbox_item.first.click()
        page.wait_for_timeout(2500)
        page.screenshot(path=os.path.join(test_dir, '04_outbox_list.png'))

    browser.close()
