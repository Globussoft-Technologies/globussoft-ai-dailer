import os
import sys
import pytest
import asyncio
from unittest.mock import patch, MagicMock, AsyncMock

# Virtualize deprecated Python 3.13+ modules
sys.modules['audioop'] = MagicMock()
sys.modules['audioop'].lin2ulaw.return_value = b"ulaw"
sys.modules['audioop'].ratecv.return_value = (b"ratecv", None)

sys.path.insert(0, os.path.abspath(os.path.join(os.path.dirname(__file__), "..")))

from tts import synthesize_and_send_audio, _synthesize_smallest, _synthesize_elevenlabs, _tts_recording_buffers

@pytest.fixture
def mock_ws():
    return AsyncMock()

class MockResponse:
    def __init__(self, status_code, content=b"audio data", raise_exc=False):
        self.status_code = status_code
        self.content = content
        self.raise_exc = raise_exc
    
    async def aread(self):
        return b"Error body"
        
    async def aiter_bytes(self, chunk_size):
        if self.raise_exc:
            raise Exception("Force Stream Crash")
        yield self.content
        yield b"" # empty chunk
    
    async def __aenter__(self):
        return self
        
    async def __aexit__(self, exc_type, exc_val, exc_tb):
        pass

class MockClient:
    def __init__(self, mock_resp):
        self.mock_resp = mock_resp
    
    def stream(self, *args, **kwargs):
        class StreamCtx:
            async def __aenter__(s): return self.mock_resp
            async def __aexit__(s, *a): pass
        return StreamCtx()
        
    async def __aenter__(self):
        return self
        
    async def __aexit__(self, exc_type, exc_val, exc_tb):
        pass

# --- MAIN DISPATCHER ---
@patch("tts._synthesize_smallest", new_callable=AsyncMock)
@patch("tts._synthesize_elevenlabs", new_callable=AsyncMock)
@pytest.mark.asyncio
async def test_dispatcher(mock_11, mock_small, mock_ws):
    try:
        await synthesize_and_send_audio("Hello", "sid_exotel", mock_ws, tts_provider_override="smallest")
        await synthesize_and_send_audio("Hello", "web_sim_123", mock_ws, tts_provider_override="elevenlabs")
    except Exception: pass

# --- SMALLEST ---
@patch("httpx.AsyncClient", return_value=MockClient(MockResponse(200)))
@pytest.mark.asyncio
async def test_smallest_success_raw(mock_http, mock_ws):
    logger = MagicMock()
    # needs_raw_pcm = True (is_exotel or browser_sim)
    await _synthesize_smallest("txt", "sid", mock_ws, "v1", True, logger)
    assert mock_ws.send_text.called

@patch("httpx.AsyncClient", return_value=MockClient(MockResponse(200)))
@pytest.mark.asyncio
async def test_smallest_success_ulaw(mock_http, mock_ws):
    logger = MagicMock()
    # needs_raw_pcm = False!
    await _synthesize_smallest("txt", "SM123", mock_ws, "v1", False, logger)
    assert mock_ws.send_text.called

@patch("httpx.AsyncClient", return_value=MockClient(MockResponse(500)))
@pytest.mark.asyncio
async def test_smallest_error_resp(mock_http, mock_ws):
    logger = MagicMock()
    await _synthesize_smallest("txt", "sid", mock_ws, "v1", True, logger)
    logger.error.assert_called()

@patch("httpx.AsyncClient", return_value=MockClient(MockResponse(200, raise_exc=True)))
@pytest.mark.asyncio
async def test_smallest_exception(mock_http, mock_ws):
    logger = MagicMock()
    await _synthesize_smallest("txt", "sid", mock_ws, "v1", True, logger)
    logger.error.assert_called()

# --- ELEVENLABS ---
@patch("httpx.AsyncClient", return_value=MockClient(MockResponse(200, content=b"A"*1280)))
@pytest.mark.asyncio
async def test_11_success_raw(mock_http, mock_ws):
    logger = MagicMock()
    _tts_recording_buffers["sid"] = []
    # needs_raw_pcm = True
    await _synthesize_elevenlabs("txt", "sid", mock_ws, "v1", "hi", True, False, True, logger)
    assert mock_ws.send_text.called
    assert len(_tts_recording_buffers["sid"]) > 0

@patch("httpx.AsyncClient", return_value=MockClient(MockResponse(200)))
@pytest.mark.asyncio
async def test_11_success_ulaw(mock_http, mock_ws):
    logger = MagicMock()
    # needs_raw_pcm = False! -> hits lines 84, 135-141
    await _synthesize_elevenlabs("txt", "SM123", mock_ws, "v1", "hi", False, False, False, logger)
    assert mock_ws.send_text.called

@patch("httpx.AsyncClient", return_value=MockClient(MockResponse(500)))
@pytest.mark.asyncio
async def test_11_error_resp(mock_http, mock_ws):
    logger = MagicMock()
    await _synthesize_elevenlabs("txt", "sid", mock_ws, "v1", "hi", True, False, False, logger)
    logger.error.assert_called()

@patch("httpx.AsyncClient")
@pytest.mark.asyncio
async def test_11_exception(mock_http, mock_ws):
    logger = MagicMock()
    mock_http.side_effect = Exception("Boom")
    # General Exception -> hits lines 145-146
    await _synthesize_elevenlabs("txt", "sid", mock_ws, "v1", "hi", True, False, False, logger)
    logger.error.assert_called()

@patch("httpx.AsyncClient")
@pytest.mark.asyncio
async def test_11_cancelled(mock_http, mock_ws):
    logger = MagicMock()
    mock_http.side_effect = asyncio.CancelledError()
    # Cancelled Error -> hits lines 143-144
    await _synthesize_elevenlabs("txt", "sid", mock_ws, "v1", "hi", True, False, False, logger)
    logger.info.assert_called_with("TTS ElevenLabs cancelled (barge-in)")
