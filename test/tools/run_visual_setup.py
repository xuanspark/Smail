import time, os
from playwright.sync_api import sync_playwright

test_log_dir = os.path.join(os.getcwd(), 'test-log', 'cloud-deploy-' + str(int(time.time())))
os.makedirs(test_log_dir, exist_ok=True)

with sync_playwright() as p:
    print('启动可视化浏览器...')
    browser = p.chromium.launch(
        headless=False,
        slow_mo=800,
        args=['--start-maximized', '--no-sandbox']
    )
    context = browser.new_context(viewport=None)
    page = context.new_page()

    print('打开: http://47.108.78.12/')
    page.goto('http://47.108.78.12/', wait_until='networkidle')
    time.sleep(1)
    page.screenshot(path=os.path.join(test_log_dir, 'step0_welcome.png'))

    # Step 0 -> 1: 点击欢迎界面的下一步 (id=next)
    print('点击欢迎页 #next...')
    page.locator('#next').click()
    time.sleep(2)
    page.screenshot(path=os.path.join(test_log_dir, 'step1_database.png'))

    # Step 1: 数据库选择 (默认 SQLite3)
    print('数据库页 #next (SQLite3)...')
    page.locator('#next').click()
    time.sleep(2)
    page.screenshot(path=os.path.join(test_log_dir, 'step2_admin_password.png'))

    # Step 2: 输入管理员密码
    print('输入管理员密码 10102442Lb!...')
    pwd_inputs = page.locator('.form input')
    print('密码页输入框数量:', pwd_inputs.count())
    for i in range(pwd_inputs.count()):
        pwd_inputs.nth(i).fill('10102442Lb!')
    
    time.sleep(1)
    page.screenshot(path=os.path.join(test_log_dir, 'step2_password_filled.png'))
    page.locator('#next').click()
    time.sleep(2)
    page.screenshot(path=os.path.join(test_log_dir, 'step3_domain.png'))

    # Step 3: 设置域名
    print('设置域名: web=mail.solit.cn, smtp=solit.cn...')
    domain_inputs = page.locator('.form input')
    print('域名页输入框数量:', domain_inputs.count())
    if domain_inputs.count() >= 2:
        domain_inputs.nth(0).fill('mail.solit.cn')
        domain_inputs.nth(1).fill('solit.cn')
    
    time.sleep(1)
    page.screenshot(path=os.path.join(test_log_dir, 'step3_domain_filled.png'))
    page.locator('#next').click()
    time.sleep(3)
    page.screenshot(path=os.path.join(test_log_dir, 'step4_dns_records.png'))

    # Step 4: DNS 设置预览
    print('DNS 记录预览页，等待 3 秒观察...')
    time.sleep(3)
    page.locator('#next').click()
    time.sleep(3)
    page.screenshot(path=os.path.join(test_log_dir, 'step5_ssl.png'))

    print('已成功推进至 SSL 设置页，保持窗口开启供查看 15 秒...')
    time.sleep(15)
    browser.close()
