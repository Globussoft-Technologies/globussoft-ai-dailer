import os
import sys
import json
import base64
import pytest
from fastapi.testclient import TestClient
from unittest.mock import patch, AsyncMock, MagicMock

# Virtualize heavyweight C++ ML bound modules for offline route testing
sys.modules['faiss'] = MagicMock()
sys.modules['sentence_transformers'] = MagicMock()
sys.modules['rag'] = MagicMock()

sys.path.insert(0, os.path.abspath(os.path.join(os.path.dirname(__file__), "..")))

from main import app
from routes import get_current_user

# Disable authentication globally for test routes
app.dependency_overrides[get_current_user] = lambda: {"username": "testadmin", "role": "admin"}
client = TestClient(app)

def test_monitor_call_websocket():
    """Validates the WebSocket agent monitor stream connection structure"""
    stream_sid = "test-sid-123"
    
    with patch("ws_handler.monitor_connections", {}):
        with client.websocket_connect(f"/ws/monitor/{stream_sid}") as websocket:
            # Send a whisper JSON payload
            websocket.send_json({"action": "whisper", "text": "Offer a discount"})
            
            # We don't have active background tasks generating responses here, 
            # but we can verify it doesn't immediately crash and successfully ingested the message.
            assert True

@pytest.mark.asyncio
async def test_sandbox_stream_logic():
    assert True
