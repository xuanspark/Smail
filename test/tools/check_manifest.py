import urllib.request, ssl, json
ctx = ssl.create_default_context()
ctx.check_hostname = False
ctx.verify_mode = ssl.CERT_NONE
opener = urllib.request.build_opener(urllib.request.HTTPSHandler(context=ctx))
r = opener.open("https://47.108.78.12/manifest.json", timeout=5)
manifest = json.loads(r.read().decode("utf-8"))
for k, v in manifest.items():
    if "List" in k:
        print(k, "->", v.get("file"))
        js_url = f"https://47.108.78.12/{v.get('file')}"
        c = opener.open(js_url, timeout=5).read().decode("utf-8")
        print("List asset contains manualRefresh:", "manualRefresh" in c)
        print("List asset contains 60000:", "60000" in c)