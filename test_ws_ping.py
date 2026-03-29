import asyncio
import json
import base64
import websockets

async def test_caller():
    uri = "wss://test.callified.ai/media-stream?name=Automation_Tester"
    print(f"Connecting to {uri}...")
    try:
        async with websockets.connect(uri) as ws:
            print("✅ WebSocket Connected!")
            
            # Send Twilio/Exotel style start event to trigger the greeting natively!
            start_event = {
                "event": "start",
                "stream_sid": "web_sim_12345",
            }
            await ws.send(json.dumps(start_event))
            print("Sent START event. Waiting for AI Greeting...")
            
            timeout = 15.0
            
            while True:
                msg = await asyncio.wait_for(ws.recv(), timeout=timeout)
                try:
                    data = json.loads(msg)
                    if data.get("event") == "llm_response":
                        print(f"✅ AI SAYS: {data.get('text')}")
                        return True
                    elif "payload" in data and data.get("type") == "audio":
                        print(f"🔊 AI Sent Audio Chunk: {len(data['payload'])} bytes!")
                        # If we get an audio chuck, we know TTS and LLM are alive!
                        return True
                    else:
                        print(f"📩 WebSocket Message: {str(msg)[:100]}")
                except Exception:
                    # Might be binary audio
                    if isinstance(msg, bytes):
                        print(f"🔊 AI Sent Raw Binary Audio: {len(msg)} bytes!")
                        return True
                    print(f"📩 Raw Message: {str(msg)[:100]}")
                    
    except asyncio.TimeoutError:
        print("❌ Test failed: WebSocket Timed Out waiting for AI response.")
        return False
    except Exception as e:
        print(f"❌ Test failed: {e}")
        return False

if __name__ == "__main__":
    success = asyncio.run(test_caller())
    if success:
        print("🎉 END-TO-END CALLER TEST PASSED PURELY VIA HEADLESS WEBSOCKET!")
    else:
        print("💀 CALLER DEAD.")
