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

# 调用管理接口为 testuser2 (id=2) 生成 recovery codes
gen_payload = json.dumps({"user_id": 2, "count": 5}).encode("utf-8")
req_gen = urllib.request.Request("https://47.108.78.12/api/v1/user/security/recovery-codes/generate", data=gen_payload, headers=headers)
resp_gen = opener.open(req_gen, timeout=5)
data = json.loads(resp_gen.read().decode("utf-8"))
print("Generate recovery codes response:", data)
