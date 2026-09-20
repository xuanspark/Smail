import os
import urllib.request, urllib.parse, hmac, hashlib, base64, time, json

ak = os.environ['SMAIL_ALIDNS_ACCESS_KEY_ID']
sk = os.environ['SMAIL_ALIDNS_ACCESS_KEY_SECRET']

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
    sorted_params = sorted(all_params.items())
    query_str = urllib.parse.urlencode(sorted_params)
    str_to_sign = 'GET&%2F&' + urllib.parse.quote(query_str, safe='')
    h = hmac.new((sk + '&').encode('utf-8'), str_to_sign.encode('utf-8'), hashlib.sha1)
    signature = base64.b64encode(h.digest()).decode('utf-8')
    all_params['Signature'] = signature
    url = 'https://alidns.aliyuncs.com/?' + urllib.parse.urlencode(all_params)
    req = urllib.request.Request(url)
    try:
        with urllib.request.urlopen(req) as response:
            return json.loads(response.read().decode('utf-8'))
    except urllib.error.HTTPError as e:
        print('Aliyun error body:', e.read().decode('utf-8'))
        raise e

dkim_pub = 'MIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKCAQEAm1F4k0SRUf0aAkIAJewM/oNzARx0XJjCXbA+HZNpqWg0rFpmGzNUsdXaD6Lw3VGok9P2JaE2t8wQxdIqZGX8f1WjXSEYkN2jv4mY2MTECy4Git8gwlwgXG4oBNcfdQ4MXJj+5YYRuqxAZPKhVoJgWbZwpHWmgdoKIoPHJVBRZQ00+K18WqYJC+kXQYGiB6+ztbYIhlZlvgoaPcvApMJE7TdhCulHKSG0DSz+lX4GaYzxGUZuGgKIb5lUBeatSsKsM8WH2eNG2NP+zS/HIdWsppkUh6u4m8V+8K3ieMENPxOsAJ9DSg1GFtCf1WU7KqTmUeQsApZOxbaHWn0W+6nfjwIDAQAB'
dkim_val = 'v=DKIM1; k=rsa; p=' + dkim_pub
dmarc_val = 'v=DMARC1; p=quarantine; rua=mailto:admin@solit.cn'

records = aliyun_request({'Action': 'DescribeDomainRecords', 'DomainName': 'solit.cn', 'PageSize': 50})
existing = { (r.get('RR'), r.get('Type')): r.get('RecordId') for r in records.get('DomainRecords', {}).get('Record', []) }
print('Existing RRs:', list(existing.keys()))

if ('_dmarc', 'TXT') not in existing:
    print('Adding _dmarc...')
    res = aliyun_request({
        'Action': 'AddDomainRecord',
        'DomainName': 'solit.cn',
        'RR': '_dmarc',
        'Type': 'TXT',
        'Value': dmarc_val,
        'TTL': 600
    })
    print('Added _dmarc result:', res)

if ('default._domainkey', 'TXT') not in existing:
    print('Adding default._domainkey...')
    res = aliyun_request({
        'Action': 'AddDomainRecord',
        'DomainName': 'solit.cn',
        'RR': 'default._domainkey',
        'Type': 'TXT',
        'Value': dkim_val,
        'TTL': 600
    })
    print('Added default._domainkey result:', res)
