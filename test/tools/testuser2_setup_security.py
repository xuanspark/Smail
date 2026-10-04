import urllib.request, ssl, json, http.cookiejar
ctx = ssl.create_default_context()
ctx.check_hostname = False
ctx.verify_mode = ssl.CERT_NONE
cj = http.cookiejar.CookieJar()
opener = urllib.request.build_opener(urllib.request.HTTPSHandler(context=ctx), urllib.request.HTTPCookieProcessor(cj))

# 1. 登录 testuser2
login_payload = json.dumps({"account": "testuser2", "password": "Password123!"}).encode("utf-8")
req = urllib.request.Request("https://47.108.78.12/api/v1/login", data=login_payload, headers={
    "Content-Type": "application/json",
    "Host": "mail.example.com",
    "Origin": "https://mail.example.com"
})
resp = opener.open(req, timeout=5)
data = json.loads(resp.read().decode())
print("Testuser2 login resp:", data)

csrf_token = ""
for c in cj:
    if c.name == "smail_csrf":
        csrf_token = c.value

headers = {
    "Content-Type": "application/json",
    "Host": "mail.example.com",
    "Origin": "https://mail.example.com",
    "Referer": "https://mail.example.com/",
    "X-CSRF-Token": csrf_token
}

# 2. 如果需要强制修改密码，先修改密码
if data.get("data", {}).get("must_change_password"):
    print("Testuser2 must change password, updating...")
    pwd_payload = json.dumps({
        "old_password": "Password123!",
        "new_password": "UserPass1234!"
    }).encode("utf-8")
    req_chg = urllib.request.Request("https://47.108.78.12/api/v1/settings/modify_password", data=pwd_payload, headers=headers)
    resp_chg = opener.open(req_chg, timeout=5)
    print("Change password resp:", resp_chg.read().decode())

# 3. 设置密保问题
q_payload = json.dumps({
    "question": "毕业学校",
    "answer": "清华大学"
}).encode("utf-8")
req_q = urllib.request.Request("https://47.108.78.12/api/v1/security/credentials/question", data=q_payload, headers=headers)
resp_q = opener.open(req_q, timeout=5)
print("Set question resp:", resp_q.read().decode())

# 4. 生成一组离线恢复码
codes_payload = json.dumps({"count": 5}).encode("utf-8")
req_c = urllib.request.Request("https://47.108.78.12/api/v1/security/credentials/recovery-codes", data=codes_payload, headers=headers)
resp_c = opener.open(req_c, timeout=5)
c_data = json.loads(resp_c.read().decode())
print("Generate recovery codes resp:", c_data)
if c_data.get("data", {}).get("codes"):
    with open("test/tools/testuser2_codes.json", "w", encoding="utf-8") as f:
        json.dump(c_data["data"], f, ensure_ascii=False, indent=2)
