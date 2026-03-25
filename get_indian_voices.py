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
        
        print("--- Indian Voices ---")
        for v in data.get("voices", []):
            tags = v.get("tags", {})
            if tags.get("accent") == "indian" or "hindi" in tags.get("language", []):
                print(f"ID: {v.get('voiceId')} | Name:     {v.get('displayName')} | Gender: {tags.get('gender')} | Lang:     {tags.get('language')}")
                
except urllib.error.URLError as e:
    print(f"Error fetching voices: {e}")
