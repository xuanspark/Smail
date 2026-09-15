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

with open('test/tools/testuser2_credential_info.json', 'r', encoding='utf-8') as f:
    cred = json.load(f)

account = cred['account']
question = cred['question']
answer = cred['answer']
# 使用尚未被消费的第 2 个恢复码
code = cred['codes'][1]
new_pwd = 'Reset888!'

write_log('====================================================')
write_log('=== 开始普通用户忘记密码（离线恢复方式）端到端真实验收测试 ===')
write_log('====================================================')
write_log(f'目标测试账号: {account}')
write_log(f'密保问题: {question}')
write_log(f'密保答案: {answer}')
write_log(f'离线恢复码: {code}')
write_log(f'设定的新密码: {new_pwd}')

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

    # 1. 访问登录页面
    write_log('1. 打开登录页面 https://mail.solit.cn/#/login')
    page.goto('https://mail.solit.cn/#/login', wait_until='networkidle')
    page.wait_for_timeout(1000)
    page.screenshot(path=os.path.join(output_dir, '20_login_page.png'))

    # 2. 点击「忘记密码？」
    forgot_btn = page.locator('.forgot-password-btn')
    write_log(f'   登录页检测到【忘记密码？】入口数量: {forgot_btn.count()}')
    forgot_btn.click()
    page.wait_for_timeout(1000)
    write_log(f'   点击后跳转 URL: {page.url}')
    page.screenshot(path=os.path.join(output_dir, '21_forgot_password_page.png'))

    # 3. 检查安全警示信息
    alert_box = page.locator('.reset-boundary')
    if alert_box.count() > 0:
        write_log(f'   超级管理员限制警示框: {alert_box.inner_text().strip()}')

    # 4. 切换到「离线恢复方式」
    switch_btn = page.locator('.switch-button')
    write_log(f'   当前模式切换按钮文案: {switch_btn.inner_text().strip()}')
    switch_btn.click()
    page.wait_for_timeout(500)
    write_log(f'   切换后切换按钮文案: {switch_btn.inner_text().strip()}')
    page.screenshot(path=os.path.join(output_dir, '22_switched_to_offline.png'))

    # 5. 填写离线恢复表单各项
    write_log('2. 填写离线密码恢复表单项...')
    inputs = page.locator('.recovery-form input')
    inputs.nth(0).fill(account)
    inputs.nth(1).fill(question)
    inputs.nth(2).fill(answer)
    inputs.nth(3).fill(code)

    page.wait_for_timeout(500)
    page.screenshot(path=os.path.join(output_dir, '23_step1_form_completed.png'))
    write_log('   步骤 1 身份核验表单填写完毕，点击【下一步】')

    # 提交步骤 1
    step1_btn = page.locator('.recovery-footer button.recovery-action-btn')
    step1_btn.click()

    page.wait_for_timeout(2500)
    page.screenshot(path=os.path.join(output_dir, '24_step2_reset_password.png'))

    write_log('3. 进入步骤 2：设置新密码，填写密码...')
    step2_inputs = page.locator('.password-grid-vertical input')
    step2_inputs.nth(0).fill(new_pwd)
    step2_inputs.nth(1).fill(new_pwd)
    page.wait_for_timeout(500)
    page.screenshot(path=os.path.join(output_dir, '25_step2_filled.png'))

    write_log('   步骤 2 密码填写完毕，点击【下一步】提交重置...')
    step2_btn = page.locator('.step-content .recovery-footer button.recovery-action-btn')
    step2_btn.click()

    page.wait_for_timeout(2500)
    page.screenshot(path=os.path.join(output_dir, '26_step3_success.png'))
    write_log('4. 进入步骤 3：完成重置，点击【重新登录】')
    relogin_btn = page.locator('.step-success button.recovery-action-btn')
    relogin_btn.click()
    page.wait_for_timeout(2000)

    msg_items = page.locator('.el-message')
    for i in range(msg_items.count()):
        write_log(f'   页面通知消息: {msg_items.nth(i).inner_text().strip()}')

    write_log(f'   重置后当前页面 URL: {page.url}')

    # 7. 自动回到登录页后，使用新密码执行登录验证
    if '#/login' in page.url:
        write_log('4. 已成功自动返回登录页，开始验证新密码登入...')
        # 页面重新加载后输入
        page.wait_for_timeout(1000)
        login_user = page.locator('.login-form input[type=\"text\"], input[placeholder*=\"账号\"], input[placeholder*=\"用户名\"]').first
        login_pwd = page.locator('.login-form input[type=\"password\"], input[placeholder*=\"密码\"]').first
        
        login_user.fill(account)
        login_pwd.fill(new_pwd)
        page.screenshot(path=os.path.join(output_dir, '25_input_new_password.png'))

        login_submit = page.locator('.login-button, button:has-text(\"登录\")').first
        login_submit.click()
        page.wait_for_timeout(3500)

        write_log(f'   新密码登录后 URL: {page.url}')
        page.screenshot(path=os.path.join(output_dir, '26_logged_in_success.png'))

        # 检查是否成功进入邮箱主界面
        if '#/' in page.url and '#/login' not in page.url:
            write_log('   [SUCCESS] 新密码验证成功！顺利进入系统主界面。')
        else:
            write_log('   [WARNING] 登录后未进入预期邮箱主页，请检查。')
    else:
        write_log(f'   [FAILED] 重置后未返回登录页，当前停留于: {page.url}')

    browser.close()
    write_log('=== 忘记密码全流程端到端测试结束 ===')