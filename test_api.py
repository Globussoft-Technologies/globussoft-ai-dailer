import urllib.request
import json
import sys

def test_url(url):
    print(f"Testing {url} ...")
    data = json.dumps({'email':'sumit@globussoft.com','password':'sumit1234'}).encode('utf-8')
    req = urllib.request.Request(url, data=data, headers={'Content-Type': 'application/json'})
    try:
        with urllib.request.urlopen(req, timeout=5) as resp:
            print('STATUS:', resp.status)
            print('BODY:', resp.read().decode('utf-8')[:200])
    except urllib.error.HTTPError as e:
        print('HTTP ERROR:', e.code)
        body = e.read().decode('utf-8')[:200]
        print('BODY:', body)
    except Exception as e:
        print('ERROR:', e)

test_url('https://test.callified.ai/api/auth/login')
print("-" * 40)
test_url('http://163.227.174.141:8001/api/auth/login')
