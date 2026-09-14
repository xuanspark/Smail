import urllib.request, ssl, re
ctx = ssl.create_default_context()
ctx.check_hostname = False
ctx.verify_mode = ssl.CERT_NONE
opener = urllib.request.build_opener(urllib.request.HTTPSHandler(context=ctx))
req = opener.open("https://47.108.78.12/", timeout=5)
html = req.read().decode("utf-8")
js_files = re.findall(r"/assets/[a-zA-Z0-9_.-]+.js", html)
print("Referenced JS files:", js_files)
for j in js_files:
    r = opener.open(f"https://47.108.78.12{j}", timeout=5)
    txt = r.read().decode("utf-8")
    print(f"{j}: size {len(txt)}, contains refresh: {'refresh' in txt}")