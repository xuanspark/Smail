import urllib.request, ssl, json, http.cookiejar
ctx = ssl.create_default_context()
ctx.check_hostname = False
ctx.verify_mode = ssl.CERT_NONE
cj = http.cookiejar.CookieJar()
opener = urllib.request.build_opener(urllib.request.HTTPSHandler(context=ctx), urllib.request.HTTPCookieProcessor(cj))

for pwd in ["12345678", "123456", "testuser2", "Password123!", "Tester123456!"]:
    login_payload = json.dumps({"account": "testuser2", "password": pwd}).encode("utf-8")
    req = urllib.request.Request("https://47.108.78.12/api/v1/login", data=login_payload, headers={
        "Content-Type": "application/json",
        "Host": "mail.example.com",
        "Origin": "https://mail.example.com"
    })
    try:
        resp = opener.open(req, timeout=5)
        res = json.loads(resp.read().decode())
        print(f"Pwd {pwd} -> errorNo: {res.get('errorNo')}, errorMsg: {res.get('errorMsg')}")
    except Exception as e:
        print(f"Pwd {pwd} -> exception: {e}")
