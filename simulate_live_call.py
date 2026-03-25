import asyncio
import websockets
import json
import base64
import sounddevice as sd
import sys

# Audio matching the Exotel PCM configuration
RATE = 8000
CHANNELS = 1
CHUNK = int(RATE * 0.02)  # 20ms buffering

async def send_audio(websocket, in_stream):
    loop = asyncio.get_event_loop()
    try:
        while True:
            # Capture 20ms of audio from laptop microphone
            data, overflowed = await loop.run_in_executor(None, in_stream.read, CHUNK)
            if data:
                payload = base64.b64encode(data).decode('utf-8')
                msg = {
                    "event": "media",
                    "media": {"payload": payload}
                }
                await websocket.send(json.dumps(msg))
    except Exception as e:
        if not isinstance(e, websockets.exceptions.ConnectionClosed):
            print(f"Send audio stopped: {e}")

async def receive_audio(websocket, out_stream):
    loop = asyncio.get_event_loop()
    try:
        async for message in websocket:
            data = json.loads(message)
            if data.get("event") == "media":
                payload = data.get("media", {}).get("payload", "")
                if payload:
                    audio_bytes = base64.b64decode(payload)
                    # Play the AI's response through laptop speakers
                    await loop.run_in_executor(None, out_stream.write, audio_bytes)
            elif data.get("event") == "clear":
                print("\n[AI INTERRUPTED BY BARGE-IN]\n")
    except websockets.exceptions.ConnectionClosed:
        print("\n[Server disconnected the call]")
    except Exception as e:
        print(f"\nReceive audio stopped: {e}")

async def run_simulator():
    uri = "ws://localhost:8001/media-stream"

    try:
        websocket = await websockets.connect(uri)
    except Exception as e:
        print(f"Failed to connect to {uri}: {e}")
        print("Make sure your local server is running on port 8001!")
        return

    print("Connected! Handshaking with AI Agent...")
    await websocket.send(json.dumps({"event": "connected"}))
    
    stream_sid = "mock_simulator_terminal_8000"
    await websocket.send(json.dumps({
        "event": "start",
        "start": {"stream_sid": stream_sid},
        "stream_sid": stream_sid
    }))
    
    print("\n" + "="*50)
    print("🎙️ SIMULATOR LIVE 🎙️")
    print("Speak into your microphone. Listen to the AI.")
    print("Press Ctrl+C to terminate the call.")
    print("="*50 + "\n")

    # Start low-latency audio hardware streams
    in_stream = sd.RawInputStream(samplerate=RATE, channels=CHANNELS, dtype='int16')
    out_stream = sd.RawOutputStream(samplerate=RATE, channels=CHANNELS, dtype='int16')
    
    in_stream.start()
    out_stream.start()

    try:
        await asyncio.gather(
            send_audio(websocket, in_stream),
            receive_audio(websocket, out_stream)
        )
    except asyncio.CancelledError:
        pass
    finally:
        in_stream.stop()
        in_stream.close()
        out_stream.stop()
        out_stream.close()

if __name__ == "__main__":
    try:
        asyncio.run(run_simulator())
    except KeyboardInterrupt:
        print("\nSimulator stopped by user.")
