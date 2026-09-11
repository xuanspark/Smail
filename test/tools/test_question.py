# -*- coding: utf-8 -*-
from playwright.sync_api import sync_playwright
import time

with sync_playwright() as p:
    browser = p.chromium.launch(headless=True)
    context = browser.new_context(ignore_https_errors=True)
    page = context.new_page()
    
    page.on("request", lambda req: print("[REQ]", req.method, req.url) if "api" in req.url else None)
    page.on("response", lambda res: print("[RES]", res.status, res.url, res.text()) if "question" in res.url else None)

    # 登录
    page.goto("https://mail.solit.cn/#/login")
    page.wait_for_selector(".login-btn")
    inputs = page.locator(".login-content input")
    inputs.nth(0).fill("xuans")
    inputs.nth(1).fill("12345678")
    page.locator(".login-btn").click()
    page.wait_for_timeout(3000)

    # 直接访问设置安全路由
    page.goto("https://mail.solit.cn/#/setting?tab=security")
    page.wait_for_timeout(2000)

    # 查看当前页面按钮和文本
    print("Page URL:", page.url)

    # 点击密保问题选项卡
    page.locator(".settings-submenu-item:has-text('密保问题')").click()
    page.wait_for_timeout(1000)

    q_input = page.locator("input[placeholder*='最喜欢的颜色']")
    a_input = page.locator("input[placeholder*='请输入答案']")
    print("q_input visible:", q_input.is_visible())
    print("a_input visible:", a_input.is_visible())

    # 尝试两字符输入：90 和 12
    print("--- 测试 1：问题 '90'，答案 '12' ---")
    q_input.fill("90")
    a_input.fill("12")
    page.locator("button:has-text('设置密保问题')").click()
    page.wait_for_timeout(2000)

    # 尝试8字符输入：90 和 12345678
    print("--- 测试 2：问题 '90'，答案 '12345678' ---")
    a_input.fill("12345678")
    page.locator("button:has-text('设置密保问题')").click()
    page.wait_for_timeout(2000)

    browser.close()
