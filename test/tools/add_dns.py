import os
import urllib.request, urllib.parse, hmac, hashlib, base64, time, json, paramiko, subprocess

ak = os.environ['SMAIL_ALIDNS_ACCESS_KEY_ID']
sk = os.environ['SMAIL_ALIDNS_ACCESS_KEY_SECRET']

client = paramiko.SSHClient()
client.set_missing_host_key_policy(paramiko.RejectPolicy())
from remote_ssh import connect_remote
connect_remote(client)
stdin, stdout, stderr = client.exec_command('cat /opt/smail/ssl/dkim/dkim.priv')
priv_key = stdout.read().decode('utf-8')
client.close()

p = subprocess.Popen(['openssl', 'rsa', '-pubout', '-outform', 'PEM'], stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
out, err = p.communicate(input=priv_key.encode('utf-8'))
pub_lines = [line.strip() for line in out.decode('utf-8').splitlines() if '---' not in line and line.strip()]
dkim_pub = ''.join(pub_lines)
dkim_val = 'v=DKIM1; k=rsa; p=' + dkim_pub
print('DKIM Value:', dkim_val)

dmarc_val = 'v=DMARC1; p=quarantine; rua=mailto:admin@solit.cn'
print('DMARC Value:', dmarc_val)

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
    str_to_sign = 'GET' + chr(38) + '%2F' + chr(38) + urllib.parse.quote(query_str, safe='')
    h = hmac.new((sk + chr(38)).encode('utf-8'), str_to_sign.encode('utf-8'), hashlib.sha1)
    signature = base64.b64encode(h.digest()).decode('utf-8')
    all_params['Signature'] = signature
    url = 'https://alidns.aliyuncs.com/?' + urllib.parse.urlencode(all_params)
    req = urllib.request.Request(url)
    with urllib.request.urlopen(req) as response:
        return json.loads(response.read().decode('utf-8'))

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
else:
    print('_dmarc already exists')

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
else:
    print('default._domainkey already exists')
