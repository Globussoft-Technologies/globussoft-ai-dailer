import requests
import time

print("--- FIRING SIMULATED EXOTEL WEBHOOK TO test.callified.ai ---")
url = "https://test.callified.ai/webhook/exotel/status"
payload = {
    "CallSid": "mock_call_sid_12345",
    "RecordingUrl": "https://www.soundhelix.com/examples/mp3/SoundHelix-Song-1.mp3"
}

try:
    response = requests.post(url, data=payload)
    print(f"Status Code: {response.status_code}")
    print(f"Response Body: {response.text}")
except Exception as e:
    print(f"Connection Error: {e}")
