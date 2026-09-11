from playwright.sync_api import sync_playwright

with sync_playwright() as p:
    browser = p.chromium.launch(headless=True)
    page = browser.new_page()
    page.on(console, lambda msg: print(f[CONSOLE {msg.type}] {msg.text}))
    page.on(pageerror, lambda exc: print(f[PAGE ERROR] {exc}))
    response = page.goto(http://47.108.78.12/)
    print(Status:, response.status)
    content = page.content()
    print(HTML length:, len(content))
    next_btn = page.locator(#next)
    print(next locator count:, next_btn.count())
    buttons = page.locator(button)
    print(Total buttons:, buttons.count())
    for i in range(buttons.count()):
        print(fButton {i}: text={buttons.nth(i).inner_text()} id={buttons.nth(i).get_attribute('id')})
    browser.close()
