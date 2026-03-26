"""
tts.py — Text-to-Speech synthesis module for Callified AI Dialer.
Supports ElevenLabs and SmallestAI providers.
Handles audio format conversion (PCM, mulaw) and streaming to WebSocket.
"""
import os
import json
import base64
import asyncio
import httpx
from fastapi import WebSocket

# Module-level dict to collect TTS audio for recording per stream
_tts_recording_buffers: dict = {}


async def synthesize_and_send_audio(
    text: str, stream_sid: str, websocket: WebSocket,
    tts_provider_override: str = None, tts_voice_override: str = None, tts_language_override: str = None
):
    import logging
    import audioop
    tts_logger = logging.getLogger("uvicorn.error")
    tts_logger.info(f"TTS START: text='{text[:60]}...', sid={stream_sid}")
    is_browser_sim = stream_sid.startswith("web_sim_")
    is_exotel = not stream_sid.startswith("SM") and not is_browser_sim
    needs_raw_pcm = is_exotel or is_browser_sim

    tts_provider = (tts_provider_override or os.getenv("TTS_PROVIDER", "elevenlabs")).lower()

    if tts_provider == "smallest":
        await _synthesize_smallest(text, stream_sid, websocket, tts_voice_override, needs_raw_pcm, tts_logger)
    else:
        await _synthesize_elevenlabs(text, stream_sid, websocket, tts_voice_override, tts_language_override, needs_raw_pcm, is_exotel, is_browser_sim, tts_logger)


async def _synthesize_smallest(text, stream_sid, websocket, tts_voice_override, needs_raw_pcm, tts_logger):
    import audioop
    url = "https://waves-api.smallest.ai/api/v1/lightning/get_speech"
    headers = {
        "Authorization": f"Bearer {os.getenv('SMALLEST_API_KEY')}",
        "Content-Type": "application/json"
    }
    payload = {
        "text": text,
        "voice_id": tts_voice_override or os.getenv("SMALLEST_VOICE_ID", "emily"),
        "sample_rate": 8000,
        "add_wav_header": False,
        "speed": 1.0
    }
    tts_logger.info(f"TTS: provider=SmallestAI, needs_raw_pcm={needs_raw_pcm}")
    try:
        async with httpx.AsyncClient(timeout=30.0) as client:
            async with client.stream("POST", url, json=payload, headers=headers) as response:
                if response.status_code != 200:
                    body = await response.aread()
                    tts_logger.error(f"TTS SmallestAI error: {body[:200]}")
                    return
                chunk_count = 0
                async for chunk in response.aiter_bytes(chunk_size=1024):
                    if chunk:
                        if needs_raw_pcm:
                            b64_chunk = base64.b64encode(chunk).decode('utf-8')
                        else:
                            ulaw_chunk = audioop.lin2ulaw(chunk, 2)
                            b64_chunk = base64.b64encode(ulaw_chunk).decode('utf-8')
                        payload_key = "stream_sid" if needs_raw_pcm else "streamSid"
                        await websocket.send_text(json.dumps({
                            "event": "media",
                            payload_key: stream_sid,
                            "media": {"payload": b64_chunk}
                        }))
                        chunk_count += 1
                tts_logger.info(f"TTS SmallestAI END: sent {chunk_count} chunks.")
    except Exception as e:
        tts_logger.error(f"TTS SmallestAI Exception: {e}")


async def _synthesize_elevenlabs(text, stream_sid, websocket, tts_voice_override, tts_language_override, needs_raw_pcm, is_exotel, is_browser_sim, tts_logger):
    import audioop
    if needs_raw_pcm:
        output_format = "pcm_16000"
    else:
        output_format = "ulaw_8000"

    url = (
        f"https://api.elevenlabs.io/v1/text-to-speech/"
        f"{tts_voice_override or os.getenv('ELEVENLABS_VOICE_ID')}/stream?output_format={output_format}&optimize_streaming_latency=3"
    )
    headers = {"xi-api-key": os.getenv("ELEVENLABS_API_KEY")}
    payload = {
        "text": text,
        "model_id": "eleven_turbo_v2_5",
        "language_code": tts_language_override or "hi",
        "voice_settings": {
            "stability": 0.35,
            "similarity_boost": 0.85,
            "style": 0.1,
            "use_speaker_boost": True
        },
    }
    tts_logger.info(f"TTS: provider=ElevenLabs, is_exotel={is_exotel}, is_browser_sim={is_browser_sim}, format={output_format}")

    try:
        async with httpx.AsyncClient(timeout=30.0) as client:
            async with client.stream("POST", url, json=payload, headers=headers) as response:
                if response.status_code != 200:
                    body = await response.aread()
                    tts_logger.error(f"TTS ElevenLabs error: {body[:200]}")
                    return
                chunk_count = 0
                pcm_buffer = b""
                audio_state = None
                async for chunk in response.aiter_bytes(chunk_size=640):
                    if chunk:
                        if needs_raw_pcm:
                            pcm_buffer += chunk
                            usable = len(pcm_buffer) - (len(pcm_buffer) % 4)
                            if usable >= 1280:
                                raw = pcm_buffer[:usable]
                                pcm_buffer = pcm_buffer[usable:]
                                downsampled, audio_state = audioop.ratecv(raw, 2, 1, 16000, 8000, audio_state)
                                b64_chunk = base64.b64encode(downsampled).decode('utf-8')
                                await websocket.send_text(json.dumps({
                                    "event": "media",
                                    "stream_sid": stream_sid,
                                    "media": {"payload": b64_chunk}
                                }))
                                if stream_sid in _tts_recording_buffers:
                                    import time as _tts_t
                                    _tts_recording_buffers[stream_sid].append((_tts_t.time(), downsampled))
                                chunk_count += 1
                                await asyncio.sleep(0.020)
                        else:
                            await websocket.send_text(json.dumps({
                                "event": "media",
                                "streamSid": stream_sid,
                                "media": {"payload": base64.b64encode(chunk).decode('utf-8')}
                            }))
                            await asyncio.sleep(0.070)
                            chunk_count += 1
                tts_logger.info(f"TTS ElevenLabs END: sent {chunk_count} chunks.")
    except asyncio.CancelledError:
        tts_logger.info("TTS ElevenLabs cancelled (barge-in)")
    except Exception as e:
        tts_logger.error(f"TTS ElevenLabs Exception: {e}")
