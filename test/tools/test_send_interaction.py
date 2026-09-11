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
            '--host-resolver-rules=MAP mail.solit.cn 47.108.78.12, MAP solit.cn 47.108.78.12'
        ]
    )
    context = browser.new_context(viewport={'width': 1440, 'height': 900}, ignore_https_errors=True)
    page = context.new_page()

    page.on('response', lambda res: print('[API RES]', res.status, res.url, res.text()[:200]) if 'send' in res.url else None)

    page.goto('https://mail.solit.cn/#/login', wait_until='networkidle')
    inputs = page.locator('.login-content input')
    inputs.nth(0).fill('xuans')
    inputs.nth(1).fill('12345678')
    page.locator('button.login-btn').click()
    page.wait_for_timeout(3000)

    page.goto('https://mail.solit.cn/#/editer', wait_until='networkidle')
    page.wait_for_timeout(2000)

    # 模拟真实在 el-select 内部输入并点击创建选项
    select_box = page.locator('.border-less-select')
    select_box.click()
    page.wait_for_timeout(300)
    page.keyboard.type('testuser2@solit.cn')
    page.wait_for_timeout(500)

    # 检查下拉列表中出现的“创建”选项或按 Enter
    page.screenshot(path=os.path.join(test_dir, '20_typing_receiver.png'))
    # 查看是否有 el-select-dropdown__item
    dropdown_item = page.locator('.el-select-dropdown__item:visible')
    print('下拉选项数量:', dropdown_item.count())
    if dropdown_item.count() > 0:
        dropdown_item.first.click()
    else:
        page.keyboard.press('Enter')
    page.wait_for_timeout(500)
    page.screenshot(path=os.path.join(test_dir, '21_receiver_selected.png'))

    # 填写主题
    subject_input = page.locator('.subject-row input')
    subject_input.fill('全量测试邮件主题 - 自动化验证')

    # 填写正文
    editor = page.locator('.editor-content')
    editor.click()
    page.keyboard.type('测试内容：这是由全量验收流程发送的正式测试邮件。')
    page.wait_for_timeout(500)
    page.screenshot(path=os.path.join(test_dir, '22_ready_to_send.png'))

    # 查看标签
    tags = page.locator('.border-less-select .el-tag')
    print('已选收件人标签数量:', tags.count())

    # 点击发送
    print('点击发送邮件...')
    page.locator('.send-btn').click()
    page.wait_for_timeout(5000)
    page.screenshot(path=os.path.join(test_dir, '23_after_send.png'))
    print('当前页面 URL:', page.url)

    # 检查发件箱
    page.goto('https://mail.solit.cn/#/list?tag={"type":1,"status":-1}', wait_until='networkidle')
    page.wait_for_timeout(2000)
    page.screenshot(path=os.path.join(test_dir, '24_outbox.png'))

    browser.close()
