import urllib.request
import urllib.error
import json
import base64

api_key = "sk_fae0151e37fa3c9e13258188b932326a"

req = urllib.request.Request(
    "https://waves-api.smallest.ai/api/v1/lightning/get_speech",
    headers={"Authorization": f"Bearer {api_key}", "Content-Type": "application/json"},
    data=json.dumps({
        "text": "Hello, this is a test.",
        "voice_id": "aravind",
        "sample_rate": 8000,
        "add_wav_header": False,
        "speed": 1.0
    }).encode("utf-8"),
    method="POST"
)

try:
    with urllib.request.urlopen(req) as response:
        audio_data = response.read()
        print(f"Received {len(audio_data)} bytes representing raw audio.")
        # check first 44 bytes to see if it's a WAV header anyway
        header = audio_data[:20]
        print(f"Header: {header}")
        if b"RIFF" in header and b"WAVE" in header:
            print("WARNING: It contains a WAV header!")
        else:
            print("No WAV header detected. Raw PCM stream.")
            
        with open("test.pcm", "wb") as f:
            f.write(audio_data)
            
except urllib.error.URLError as e:
    print(f"Error: {e}")
    print(e.read().decode())
