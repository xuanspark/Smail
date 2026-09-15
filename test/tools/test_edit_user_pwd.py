import urllib.request, ssl, json, http.cookiejar
ctx = ssl.create_default_context()
ctx.check_hostname = False
ctx.verify_mode = ssl.CERT_NONE
cj = http.cookiejar.CookieJar()
opener = urllib.request.build_opener(urllib.request.HTTPSHandler(context=ctx), urllib.request.HTTPCookieProcessor(cj))

# 登录 xuans
login_payload = json.dumps({"account": "xuans", "password": "12345678"}).encode("utf-8")
req = urllib.request.Request("https://47.108.78.12/api/v1/login", data=login_payload, headers={
    "Content-Type": "application/json",
    "Host": "mail.solit.cn",
    "Origin": "https://mail.solit.cn"
})
opener.open(req, timeout=5)

csrf_token = ""
for c in cj:
    if c.name == "smail_csrf":
        csrf_token = c.value

headers = {
    "Content-Type": "application/json",
    "Host": "mail.solit.cn",
    "Origin": "https://mail.solit.cn",
    "Referer": "https://mail.solit.cn/",
    "X-CSRF-Token": csrf_token
}

edit_payload = json.dumps({
    "id": 2,
    "account": "testuser2",
    "username": "普通用户",
    "password": "Password123!",
    "disabled": 0,
    "role": "user"
}).encode("utf-8")
req_edit = urllib.request.Request("https://47.108.78.12/api/v1/user/edit", data=edit_payload, headers=headers)
resp_edit = opener.open(req_edit, timeout=5)
print("Edit user response:", resp_edit.read().decode())
