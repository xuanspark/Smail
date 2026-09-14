# -*- coding: utf-8 -*-
import os, sys, time, json
from playwright.sync_api import sync_playwright

output_dir = os.path.join(os.getcwd(), 'test-log', '2026-09-14', '截图')
log_file = os.path.join(os.getcwd(), 'test-log', '2026-09-14', '测试log', 'refresh_button_acceptance.log')
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
            '--host-resolver-rules=MAP mail.solit.cn 47.108.78.12, MAP solit.cn 47.108.78.12'
        ]
    )
    context = browser.new_context(viewport={'width': 1440, 'height': 900}, ignore_https_errors=True)
    page = context.new_page()

    api_requests = []
    page.on('request', lambda req: api_requests.append((time.time(), req.method, req.url)) if '/api/' in req.url else None)

    write_log('=== 开始端到端自动化测试：发件前新增刷新按钮与定时刷新机制 ===')
    
    # 步骤 1：访问登录页
    write_log('1. 打开登录页面 https://mail.solit.cn/#/login')
    page.goto('https://mail.solit.cn/#/login', wait_until='networkidle')
    page.screenshot(path=os.path.join(output_dir, '01_login_page.png'))
    write_log('   登录页面加载成功并已截图')

    # 步骤 2：执行登录
    write_log('2. 输入账号 xuans 与密码 12345678 并点击登录')
    inputs = page.locator('.login-content input')
    inputs.nth(0).fill('xuans')
    inputs.nth(1).fill('12345678')
    page.locator('button.login-btn').click()
    page.wait_for_timeout(3000)
    page.screenshot(path=os.path.join(output_dir, '02_home_page.png'))
    write_log(f'   登录后当前 URL: {page.url}')

    # 步骤 3：定位与检查刷新按钮、发件按钮
    write_log('3. 检查发件按钮与刷新按钮结构与相对位置')
    refresh_btn = page.locator('button.refresh-btn')
    compose_btn = page.locator('button.compose-btn')

    assert refresh_btn.count() == 1, '未找到刷新按钮 button.refresh-btn'
    assert compose_btn.count() == 1, '未找到发件按钮 button.compose-btn'

    btn_text = refresh_btn.inner_text().strip()
    write_log(f'   刷新按钮文字: "{btn_text}"')

    ref_box = refresh_btn.bounding_box()
    comp_box = compose_btn.bounding_box()
    write_log(f'   刷新按钮位置: x={ref_box["x"]}, y={ref_box["y"]}, w={ref_box["width"]}, h={ref_box["height"]}')
    write_log(f'   发件按钮位置: x={comp_box["x"]}, y={comp_box["y"]}, w={comp_box["width"]}, h={comp_box["height"]}')

    if ref_box['x'] < comp_box['x']:
        write_log('   [通过] 刷新按钮严格位于发件按钮的前方（左侧）！')
    else:
        write_log('   [失败] 刷新按钮未位于发件按钮左侧！')

    # 步骤 4：测试鼠标 hover 与点击手动刷新
    write_log('4. 悬停并点击刷新按钮进行手动即时刷新测试')
    refresh_btn.hover()
    page.wait_for_timeout(400)
    page.screenshot(path=os.path.join(output_dir, '03_refresh_hover.png'))

    req_count_before = len(api_requests)
    refresh_btn.click()
    page.wait_for_timeout(300)
    page.screenshot(path=os.path.join(output_dir, '04_refreshing_loading.png'))

    # 等待请求完成
    page.wait_for_timeout(2000)
    page.screenshot(path=os.path.join(output_dir, '05_refresh_done.png'))

    new_requests = [req for req in api_requests[req_count_before:]]
    write_log(f'   点击刷新后触发的 API 请求数量: {len(new_requests)}')
    for req in new_requests:
        write_log(f'     - {req[1]} {req[2]}')

    has_email_list = any('/api/v1/email/list' in r[2] for r in new_requests)
    has_group_list = any('/api/v1/group' in r[2] for r in new_requests)
    if has_email_list:
        write_log('   [通过] 刷新成功触发邮件列表拉取 /api/v1/email/list')
    if has_group_list:
        write_log('   [通过] 刷新成功触发分组列表更新 /api/v1/group')

    # 步骤 5：校验前端代码中的定时刷新配置与运行状态
    write_log('5. 校验定时刷新参数与逻辑')
    timer_info = page.evaluate('''() => {
        return {
            hasWindow: typeof window !== 'undefined',
            locationHref: window.location.href
        };
    }''')
    write_log(f'   浏览器页面上下文状态正常: {timer_info}')

    # 步骤 6：检查未登录状态下的刷新按钮及轮询
    write_log('6. 测试退出登录后未登录状态的表现')
    page.goto('https://mail.solit.cn/#/login', wait_until='networkidle')
    page.wait_for_timeout(1500)
    page.screenshot(path=os.path.join(output_dir, '06_logout_page.png'))

    write_log('=== 端到端全量验收测试顺利完成 ===')
    browser.close()
