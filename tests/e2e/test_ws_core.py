import pytest
import warnings
from fastapi.testclient import TestClient
from unittest.mock import patch, AsyncMock, MagicMock
import json
import base64
import sys
import os

# Mock packages before main imports them
sys.modules['faiss'] = MagicMock()
sys.modules['numpy'] = MagicMock()
sys.modules['rag'] = MagicMock()
sys.modules['llm_provider'] = MagicMock()

# Global mock for deepgram to prevent the real SDK from executing in test routes
mock_dg_module = MagicMock()
sys.modules['deepgram'] = mock_dg_module

sys.path.insert(0, os.path.abspath(os.path.join(os.path.dirname(__file__), '../..')))
from main import app
client = TestClient(app)

@pytest.fixture(autouse=True)
def suppress_warnings():
    warnings.filterwarnings("ignore")
    os.environ["ELEVENLABS_API_KEY"] = "dummy_key"
    os.environ["ELEVENLABS_VOICE_ID"] = "dummy_voice"
    os.environ["DEEPGRAM_API_KEY"] = "dummy_dg"
    os.environ["GEMINI_API_KEY"] = "dummy_gemini"

@pytest.fixture
def mock_deepgram():
    """Mock the entire Deepgram client and connection."""
    # We patch the instance that will be instantiated by the mocked class
    mock_instance = MagicMock()
    mock_conn = MagicMock()
    mock_conn.start = AsyncMock()
    mock_conn.finish = AsyncMock()
    mock_conn.send = AsyncMock()
    
    callbacks = {}
    def _on(event, handler):
        callbacks[event] = handler
        
    mock_v = MagicMock()
    mock_v.return_value = mock_conn
    mock_ws = MagicMock()
    mock_ws.v = mock_v
    mock_listen = MagicMock()
    mock_listen.websocket = mock_ws
    mock_instance.listen = mock_listen
    mock_conn.on.side_effect = _on
    
    # Assign our mock instance to be returned when DeepgramClient(...) is called
    sys.modules['deepgram'].DeepgramClient.return_value = mock_instance
    sys.modules['deepgram'].LiveTranscriptionEvents.Transcript = "Transcript"
    sys.modules['deepgram'].LiveTranscriptionEvents.SpeechStarted = "SpeechStarted"
    
    # Instead of an external fire, we attach it to the synchronous flow of data
    # When the backend sends bytes to Deepgram (via send), we immediately mock a transcript arriving.
    async def _mock_send(data, **kwargs):
        if "Transcript" in callbacks:
            # Must simulate finding "Is the pricing high?" or "Yes I filled the form." depending on test
            if b'hello world' in data:
                res = "Is the pricing high?"
            else:
                res = "Yes I filled the form."
            from tests.e2e.test_ws_core import DummyResult
            coro = callbacks["Transcript"](None, DummyResult(res))
            if coro:
                await coro
                
    mock_conn.send = AsyncMock(side_effect=_mock_send)

    yield {
        "cls": sys.modules['deepgram'].DeepgramClient,
        "instance": mock_instance,
        "conn": mock_conn
    }

@pytest.fixture
def mock_llm_provider():
    sys.modules['llm_provider'].generate_response = AsyncMock(return_value="[MOCK] E2E response")
    
    async def mock_resp_stream(*args, **kwargs):
        yield "Hello "
        yield "world."
    sys.modules['llm_provider'].generate_response_stream = mock_resp_stream
    yield

@pytest.fixture(autouse=True)
def mock_elevenlabs():
    # Mock elevenlabs streaming call safely globally so tts.py doesn't hang
    class FakeResponseContext:
        async def __aenter__(self):
            class FakeResponse:
                status_code = 200
                async def aiter_bytes(self, chunk_size):
                    yield b"FAKE_AUDIO_BYTES_01"
            return FakeResponse()
        async def __aexit__(self, exc_type, exc_val, exc_tb):
            pass
    
    class FakeClient:
        def stream(self, *args, **kwargs):
            return FakeResponseContext()
        async def __aenter__(self): return self
        async def __aexit__(self, exc_type, exc_val, exc_tb): pass
    
    with patch("ws_handler.httpx.AsyncClient", return_value=FakeClient()), \
         patch("tts.httpx.AsyncClient", return_value=FakeClient()):
        yield

class DummyResult:
    """Mock Deepgram Result Object"""
    def __init__(self, transcript, is_final=True):
        self.is_final = is_final
        class Alt:
            def __init__(self, t):
                self.transcript = t
        class Chan:
            def __init__(self, t):
                self.alternatives = [Alt(t)]
        self.channel = Chan(transcript)

@pytest.fixture(autouse=True)
def mock_db():
    with patch("ws_handler.get_conn"), \
         patch("ws_handler.get_pronunciation_context", return_value=""), \
         patch("ws_handler.get_product_knowledge_context", return_value=""), \
         patch("ws_handler.get_org_custom_prompt", return_value=""), \
         patch("ws_handler.get_org_voice_settings", return_value={"tts_provider": "echo"}), \
         patch("ws_handler.save_call_transcript"), \
         patch("database.get_conn"):
        yield

def test_sandbox_stream_lifecycle(mock_deepgram, mock_llm_provider, mock_elevenlabs):
    """
    Test the /ws/sandbox WebSocket endpoint that simulates the browser mic.
    Requires mocking Deepgram STT transcription and ElevenLabs TTS output.
    """
    with client.websocket_connect("/ws/sandbox") as websocket:
        # Send a fake audio chunk
        fake_audio = base64.b64encode(b"hello world audio").decode()
        websocket.send_json({"type": "audio_chunk", "payload": fake_audio})
        
        # Wait for the backend to echo the transcript back
        import time
        res1 = None
        for _ in range(50):
            try:
                res1 = websocket.receive_json()
                break
            except Exception:
                 time.sleep(0.1)
        assert res1 is not None, "Websocket never received a response"
        assert res1["type"] == "transcript"
        assert res1["role"] == "user"
        assert "pricing high" in res1["text"]
        
        # Wait for the LLM to generate dummy response and ElevenLabs fake audio to stream back
        res2 = websocket.receive_json()
        assert res2["type"] == "audio"
        assert res2["payload"]  # Base64 encoded fake audio bytes
        
        # Eventually it echoes the final AI text transcript
        res3 = websocket.receive_json()
        if res3["type"] == "audio":
             res3 = websocket.receive_json()
        
        assert res3["type"] == "transcript"
        assert res3["role"] == "agent"
        assert "[MOCK]" in res3["text"]


def test_exotel_media_stream_telephony(mock_deepgram, mock_llm_provider):
    """
    Test the raw byte /media-stream loop utilized by Exotel production calls.
    It expects 'event: start' and binary audio streams.
    """
    # 1. Provide a mocked connection that simulates Twilio/Exotel
    with patch("ws_handler.get_org_voice_settings", return_value={"tts_provider": "echo"}):
        with client.websocket_connect("/media-stream?name=Test_Lead&phone=919999999999") as websocket:
            # Send standard Exotel start signature
            websocket.send_text(json.dumps({
                "event": "start",
                "call_sid": "mocked-exotel-123",
                "stream_sid": "mocked-stream-123"
            }))
            
            # Immediately the backend should synthesize a Greeting using TTS
            # The TTS in ws_handler streams base64 via twilio format if not overridden but here we just listen
            # We must await the asyncio TTS task yielding the greeting. Since it uses "echo"/fake tts, 
            # we should see some json media payloads coming back soon.
            
            # Exotel sends binary raw audio frames usually (or base64 if twilio)
            websocket.send_bytes(b"sys bytes testing")
            
            # Wait for text echoed back via ws_handler or AI's audio response
            media_received = False
            for _ in range(10):
                import time
                try:
                    msg = websocket.receive_json()
                    if msg.get("event") == "media":
                        media_received = True
                        break
                except Exception:
                    time.sleep(0.1)
            
            assert media_received, "Backend did not execute pipeline and return TTS media payloads."
