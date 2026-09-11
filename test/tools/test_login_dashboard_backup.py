import time, os
from playwright.sync_api import sync_playwright

test_log_dir = os.path.join(os.getcwd(), "test-log", "login-and-dashboard-v2")
os.makedirs(test_log_dir, exist_ok=True)

with sync_playwright() as p:
    print("启动可视化真实浏览器...")
    browser = p.chromium.launch(
        headless=False,
        slow_mo=800,
        args=["--start-maximized", "--no-sandbox", "--ignore-certificate-errors"]
    )
    context = browser.new_context(viewport=None, ignore_https_errors=True)
    page = context.new_page()

    print("访问登录�? https://47.108.78.12/#/login")
    page.goto("https://47.108.78.12/#/login", wait_until="networkidle")
    time.sleep(2)
    page.screenshot(path=os.path.join(test_log_dir, "01_login_page.png"))

    print("输入管理员账号密�? xuans / 12345678")
    inputs = page.locator("input")
    print("输入框数�?", inputs.count())
    inputs.nth(0).fill("xuans")
    inputs.nth(1).fill("12345678")
    time.sleep(1)
    page.screenshot(path=os.path.join(test_log_dir, "02_credentials_filled.png"))

    print("点击登录按钮...")
    page.locator("button").filter(has_text="登录").first.click()
    time.sleep(3)
    page.screenshot(path=os.path.join(test_log_dir, "03_after_login_action.png"))
    print("当前页面 URL:", page.url)
    time.sleep(2)
    page.screenshot(path=os.path.join(test_log_dir, "04_security_dialog_or_dashboard.png"))
    time.sleep(5)
    browser.close()
