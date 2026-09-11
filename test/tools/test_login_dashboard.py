import time, os
from playwright.sync_api import sync_playwright

test_log_dir = os.path.join(os.getcwd(), "test-log", "login-and-dashboard-v2")
os.makedirs(test_log_dir, exist_ok=True)

with sync_playwright() as p:
    print("鍚姩鍙鍖栫湡瀹炴祻瑙堝櫒...")
    browser = p.chromium.launch(
        headless=False,
        slow_mo=800,
        args=["--start-maximized", "--no-sandbox", "--ignore-certificate-errors"]
    )
    context = browser.new_context(viewport=None, ignore_https_errors=True)
    page = context.new_page()

    print("璁块棶鐧诲綍椤? https://47.108.78.12/#/login")
    page.goto("https://47.108.78.12/#/login", wait_until="networkidle")
    time.sleep(2)
    page.screenshot(path=os.path.join(test_log_dir, "01_login_page.png"))

    print("杈撳叆绠＄悊鍛樿处鍙峰瘑鐮? xuans / 12345678")
    inputs = page.locator("input")
    print("杈撳叆妗嗘暟閲?", inputs.count())
    inputs.nth(0).fill("xuans")
    inputs.nth(1).fill("12345678")
    time.sleep(1)
    page.screenshot(path=os.path.join(test_log_dir, "02_credentials_filled.png"))

    print("鐐瑰嚮鐧诲綍鎸夐挳...")
    page.locator(".login-btn").click()
    time.sleep(3)
    page.screenshot(path=os.path.join(test_log_dir, "03_after_login_action.png"))
    print("褰撳墠椤甸潰 URL:", page.url)
    time.sleep(2)
    page.screenshot(path=os.path.join(test_log_dir, "04_security_dialog_or_dashboard.png"))
    time.sleep(5)
    browser.close()
