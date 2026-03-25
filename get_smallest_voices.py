import urllib.request
import urllib.error
import json

api_key = "sk_fae0151e37fa3c9e13258188b932326a"

req = urllib.request.Request(
    "https://api.smallest.ai/waves/v1/lightning/get_voices",
    headers={"Authorization": f"Bearer {api_key}"}
)

try:
    with urllib.request.urlopen(req) as response:
        data = json.loads(response.read().decode())
        print(json.dumps(data, indent=2))
except urllib.error.URLError as e:
    print(f"Error fetching voices: {e}")
