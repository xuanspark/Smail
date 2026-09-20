from playwright.sync_api import sync_playwright

import os

with sync_playwright() as p:
    browser = p.chromium.launch(headless=True)
    page = browser.new_page()
    page.on('console', lambda msg: print(f'控制台类型：{msg.type}'))
    page.on('pageerror', lambda exc: print(f'页面错误类型：{type(exc).__name__}'))
    response = page.goto(os.environ['SMAIL_TEST_URL'])
    print('状态码：', response.status)
    content = page.content()
    print('页面长度：', len(content))
    next_btn = page.locator('#next')
    print('下一步按钮数量：', next_btn.count())
    buttons = page.locator('button')
    print('按钮总数：', buttons.count())
    for i in range(buttons.count()):
        print(f'按钮 {i}：{buttons.nth(i).inner_text()}')
    browser.close()
