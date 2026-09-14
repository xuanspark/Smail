import urllib.request, ssl, json, http.cookiejar
ctx = ssl.create_default_context()
ctx.check_hostname = False
ctx.verify_mode = ssl.CERT_NONE
cj = http.cookiejar.CookieJar()
opener = urllib.request.build_opener(urllib.request.HTTPSHandler(context=ctx), urllib.request.HTTPCookieProcessor(cj))
login_payload = json.dumps({"account": "xuans", "password": "12345678"}).encode("utf-8")
req = urllib.request.Request("https://47.108.78.12/api/v1/login", data=login_payload, headers={"Content-Type": "application/json"})
resp = opener.open(req, timeout=5)
login_res = json.loads(resp.read().decode("utf-8"))
csrf_token = ""
for cookie in cj:
    if cookie.name == "smail_csrf":
        csrf_token = cookie.value
headers = {"Content-Type": "application/json", "X-CSRF-Token": csrf_token}
# 正常前端请求 list 携带的参数
req_email = urllib.request.Request("https://47.108.78.12/api/v1/email/list", data=json.dumps({"tag": "{\"type\":0,\"status\":-1}", "page_size": 15}).encode("utf-8"), headers=headers)
resp_email = opener.open(req_email, timeout=5)
print("Email list resp:", resp_email.read().decode("utf-8")[:300])