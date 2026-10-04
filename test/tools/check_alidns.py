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
    with urllib.request.urlopen(req) as response:
        return json.loads(response.read().decode('utf-8'))

records = aliyun_request({'Action': 'DescribeDomainRecords', 'DomainName': 'example.com', 'PageSize': 50})
print('Total records in example.com:', records.get('TotalCount'))
for r in records.get('DomainRecords', {}).get('Record', []):
    rr = r.get('RR')
    t = r.get('Type')
    v = r.get('Value')
    s = r.get('Status')
    print(f'{rr:<22} | {t:<6} | {s:<8} | {v}')
