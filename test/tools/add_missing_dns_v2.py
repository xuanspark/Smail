import os
import urllib.request, urllib.parse, hmac, hashlib, base64, time, json

ak = os.environ['SMAIL_ALIDNS_ACCESS_KEY_ID']
sk = os.environ['SMAIL_ALIDNS_ACCESS_KEY_SECRET']

def percent_encode(val):
    res = urllib.parse.quote(str(val), safe='')
    res = res.replace('+', '%20')
    res = res.replace('*', '%2A')
    res = res.replace('%7E', '~')
    return res

def aliyun_request(params):
    public_params = {
        'Format': 'JSON',
        'Version': '2015-01-09',
        'AccessKeyId': ak,
        'SignatureMethod': 'HMAC-SHA1',
        'Timestamp': time.strftime('%Y-%m-%dT%H:%M:%SZ', time.gmtime()),
        'SignatureVersion': '1.0',
        'SignatureNonce': str(int(time.time()*1000)),
    }
    all_params = {**public_params, **params}
    sorted_params = sorted(all_params.items(), key=lambda x: x[0])
    query_str = '&'.join([f'{percent_encode(k)}={percent_encode(v)}' for k, v in sorted_params])
    str_to_sign = 'GET&%2F&' + percent_encode(query_str)
    h = hmac.new((sk + '&').encode('utf-8'), str_to_sign.encode('utf-8'), hashlib.sha1)
    signature = base64.b64encode(h.digest()).decode('utf-8')
    final_url = f'https://alidns.aliyuncs.com/?{query_str}&Signature={percent_encode(signature)}'
    req = urllib.request.Request(final_url)
    try:
        with urllib.request.urlopen(req) as response:
            return json.loads(response.read().decode('utf-8'))
    except urllib.error.HTTPError as e:
        print('HTTP Error body:', e.read().decode('utf-8'))
        raise e

dkim_pub = 'MIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKCAQEAm1F4k0SRUf0aAkIAJewM/oNzARx0XJjCXbA+HZNpqWg0rFpmGzNUsdXaD6Lw3VGok9P2JaE2t8wQxdIqZGX8f1WjXSEYkN2jv4mY2MTECy4Git8gwlwgXG4oBNcfdQ4MXJj+5YYRuqxAZPKhVoJgWbZwpHWmgdoKIoPHJVBRZQ00+K18WqYJC+kXQYGiB6+ztbYIhlZlvgoaPcvApMJE7TdhCulHKSG0DSz+lX4GaYzxGUZuGgKIb5lUBeatSsKsM8WH2eNG2NP+zS/HIdWsppkUh6u4m8V+8K3ieMENPxOsAJ9DSg1GFtCf1WU7KqTmUeQsApZOxbaHWn0W+6nfjwIDAQAB'
dkim_val = 'v=DKIM1; k=rsa; p=' + dkim_pub
dmarc_val = 'v=DMARC1; p=quarantine; rua=mailto:admin@example.com'

print('--- 添加 _dmarc ---')
res_dmarc = aliyun_request({
    'Action': 'AddDomainRecord',
    'DomainName': 'example.com',
    'RR': '_dmarc',
    'Type': 'TXT',
    'Value': dmarc_val,
    'TTL': 600
})
print('Success:', res_dmarc)

print('--- 添加 default._domainkey ---')
res_dkim = aliyun_request({
    'Action': 'AddDomainRecord',
    'DomainName': 'example.com',
    'RR': 'default._domainkey',
    'Type': 'TXT',
    'Value': dkim_val,
    'TTL': 600
})
print('Success:', res_dkim)
