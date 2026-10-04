import time, os
from playwright.sync_api import sync_playwright

test_log_dir = os.path.join(os.getcwd(), 'test-log', 'cloud-setup-' + str(int(time.time())))
os.makedirs(test_log_dir, exist_ok=True)

with sync_playwright() as p:
    print('启动可视化浏览器，开始初始化向导流程...')
    browser = p.chromium.launch(
        headless=False,
        slow_mo=600,
        args=['--start-maximized', '--no-sandbox']
    )
    context = browser.new_context(viewport=None)
    page = context.new_page()

    print('直接访问引导页: http://47.108.78.12/#/setup')
    page.goto('http://47.108.78.12/#/setup', wait_until='networkidle')
    time.sleep(2)
    page.screenshot(path=os.path.join(test_log_dir, 'step0_welcome.png'))
    print('【步骤 0：欢迎页】截图已保存')

    # 点击下一步进入数据库设置
    print('点击“下一步”进入数据库设置...')
    page.locator('#next').click()
    time.sleep(2)
    page.screenshot(path=os.path.join(test_log_dir, 'step1_database.png'))
    print('【步骤 1：数据库设置】截图已保存，默认选用 SQLite3')

    # 点击下一步进入管理员密码设置
    print('确认 SQLite3 数据库，点击“下一步”...')
    page.locator('#next').click()
    time.sleep(2)
    page.screenshot(path=os.path.join(test_log_dir, 'step2_admin_password.png'))
    print('【步骤 2：设置管理员密码】截图已保存')

    # 输入密码
    inputs = page.locator('.database-form input, .ctn input')
    print('找到输入框数量:', inputs.count())
    for i in range(inputs.count()):
        inputs.nth(i).fill(os.environ["SMAIL_TEST_ADMIN_PASSWORD"])
    
    time.sleep(1)
    page.screenshot(path=os.path.join(test_log_dir, 'step2_password_filled.png'))
    page.locator('#next').click()
    time.sleep(2)
    page.screenshot(path=os.path.join(test_log_dir, 'step3_domain.png'))
    print('【步骤 3：设置域名】截图已保存')

    # 填写域名: Web域名与主域名
    domain_inputs = page.locator('.ctn input')
    print('域名输入框数量:', domain_inputs.count())
    if domain_inputs.count() >= 2:
        domain_inputs.nth(0).fill('mail.example.com')
        domain_inputs.nth(1).fill('example.com')
    elif domain_inputs.count() == 1:
        domain_inputs.first.fill('example.com')
    
    time.sleep(1)
    page.screenshot(path=os.path.join(test_log_dir, 'step3_domain_filled.png'))
    print('点击“下一步”提交域名配置...')
    page.locator('#next').click()
    time.sleep(3)
    page.screenshot(path=os.path.join(test_log_dir, 'step4_dns.png'))
    print('【步骤 4：DNS 设置检查】截图已保存，展示权威解析记录状态')

    # 点击下一步进入 SSL 配置
    time.sleep(3)
    print('点击“下一步”进入 SSL 证书配置...')
    page.locator('#next').click()
    time.sleep(3)
    page.screenshot(path=os.path.join(test_log_dir, 'step5_ssl.png'))
    print('【步骤 5：SSL 证书模式】截图已保存，窗口将保持 20 秒供查看...')
    time.sleep(20)
    browser.close()
