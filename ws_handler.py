"""
ws_handler.py — WebSocket media stream handler for Callified AI Dialer.
Manages Deepgram STT, LLM pipeline, greeting, barge-in, and call recording.
"""
import os
import json
import base64
import asyncio
import logging
import uuid as _uuid
import time
import httpx
from fastapi import WebSocket
from deepgram import DeepgramClient, LiveTranscriptionEvents, LiveOptions
from google import genai
from google.genai import types
import call_logger
from database import (
    get_pronunciation_context, get_product_knowledge_context,
    get_org_custom_prompt, get_org_voice_settings, save_call_transcript,
    get_conn,
)
from tts import synthesize_and_send_audio, _tts_recording_buffers
import redis_store
from prompt_builder import build_call_context
from recording_service import save_call_recording_and_transcript

# ─── Shared State ────────────────────────────────────────────────────────────
# Non-serializable state stays in-memory (asyncio.Task, WebSocket connections)
active_tts_tasks = {}
monitor_connections: dict[str, set[WebSocket]] = {}
twilio_websockets: dict[str, WebSocket] = {}

# Serializable state backed by Redis (falls back to in-memory if Redis unavailable)
# Access via redis_store.get_pending_call(), redis_store.get_takeover(), etc.
# Legacy aliases kept for backward-compat in main.py dial functions:
pending_call_info = {}

# SDK clients (initialized lazily)
dg_client = None
llm_client = None

# ─── WebSocket Handler ──────────────────────────────────────────────────────

async def handle_media_stream(websocket: WebSocket):
    global dg_client, llm_client
    await websocket.accept()

    # Try query params first, then fall back to pending_call_info from dial
    lead_name = websocket.query_params.get("name", "") or ""
    interest = websocket.query_params.get("interest", "") or ""
    lead_phone = websocket.query_params.get("phone", "") or ""
    _call_lead_id = None
    _campaign_id = None
    _qp_lead_id = websocket.query_params.get("lead_id", "")
    if _qp_lead_id and _qp_lead_id.isdigit():
        _call_lead_id = int(_qp_lead_id)
    _tts_provider_override = websocket.query_params.get("tts_provider", None) or None
    _tts_voice_override = websocket.query_params.get("voice", None) or None
    _tts_language_override = websocket.query_params.get("tts_language", None) or None

    # Check Redis pending call for campaign voice overrides (Exotel dial flow)
    if not _tts_voice_override:
        _pending_voice = redis_store.get_pending_call("latest")
        if _pending_voice.get("tts_provider"):
            _tts_provider_override = _pending_voice["tts_provider"]
        if _pending_voice.get("tts_voice_id"):
            _tts_voice_override = _pending_voice["tts_voice_id"]
        if _pending_voice.get("tts_language"):
            _tts_language_override = _pending_voice["tts_language"]

    # If still no voice override, look up org voice settings from DB
    if not _tts_voice_override:
        try:
            _vc = get_conn()
            _vcur = _vc.cursor()
            _org_for_voice = None
            if _call_lead_id:
                _vcur.execute("SELECT org_id FROM leads WHERE id = %s", (_call_lead_id,))
                _lr = _vcur.fetchone()
                if _lr and _lr.get('org_id'):
                    _org_for_voice = _lr['org_id']
            if not _org_for_voice:
                _vcur.execute("SELECT org_id FROM users LIMIT 1")
                _ur = _vcur.fetchone()
                if _ur:
                    _org_for_voice = _ur.get('org_id')
            _vc.close()
            if _org_for_voice:
                _vs = get_org_voice_settings(_org_for_voice)
                if _vs.get('tts_voice_id'):
                    _tts_voice_override = _vs['tts_voice_id']
                    _tts_provider_override = _vs.get('tts_provider', 'elevenlabs')
                _tts_language_override = _tts_language_override or _vs.get('tts_language', 'hi')
        except Exception:
            pass

    if not lead_name or lead_name == "Customer":
        # Try to look up by phone number first (supports concurrent dialing)
        info = {}
        if lead_phone:
            _phone_clean = lead_phone.lstrip("+").strip()
            info = redis_store.get_pending_call(f"phone:{_phone_clean}")
            if not info and len(_phone_clean) > 10:
                info = redis_store.get_pending_call(f"phone:{_phone_clean[-10:]}")
        if not info:
            info = redis_store.get_pending_call("latest")
        lead_name = info.get("name", "Customer")
        interest = info.get("interest", "our platform") if not interest else interest
        lead_phone = info.get("phone", "") if not lead_phone else lead_phone
        if not _call_lead_id:
            _call_lead_id = info.get("lead_id")
        # Also pick up campaign/voice from phone-matched pending call
        if info.get("campaign_id"):
            _campaign_id = info["campaign_id"]

    EXOTEL_API_KEY = (os.getenv("EXOTEL_API_KEY") or "").strip()
    EXOTEL_API_TOKEN = (os.getenv("EXOTEL_API_TOKEN") or "").strip()
    EXOTEL_ACCOUNT_SID = (os.getenv("EXOTEL_ACCOUNT_SID") or "").strip()

    _exotel_call_sid = ""
    if lead_phone:
        _phone_info = redis_store.get_pending_call(f"phone:{lead_phone.lstrip('+').strip()}")
        _exotel_call_sid = _phone_info.get("exotel_call_sid", "")
    if not _exotel_call_sid:
        _exotel_call_sid = (redis_store.get_pending_call("latest").get("exotel_call_sid") or "")
    _call_start_time = time.time()
    stream_sid = None
    is_exotel_stream = False
    chat_history = []
    _llm_lock = asyncio.Lock()
    _hangup_requested = [False]  # mutable flag to block new transcripts after [HANGUP]
    _last_transcript_time = [0.0]
    _debounce_delay = 0.05
    _recording_mic_chunks = []
    _recording_tts_chunks = []

    # Load pronunciation guide
    pronunciation_ctx = get_pronunciation_context()

    # Check for campaign context (from query params or Redis pending call)
    _campaign_id = None
    _qp_campaign = websocket.query_params.get("campaign_id", "")
    if _qp_campaign and _qp_campaign.isdigit():
        _campaign_id = int(_qp_campaign)
    if not _campaign_id:
        _pending_info = redis_store.get_pending_call("latest")
        _campaign_id = _pending_info.get("campaign_id")

    # Load product knowledge + per-product persona/call flow for system prompt
    product_ctx = ""
    _product_persona = ""
    _product_call_flow = ""
    _product_name = ""
    try:
        _user_conn = get_conn()
        _user_cursor = _user_conn.cursor()
        _user_cursor.execute("SELECT org_id FROM leads WHERE id = %s LIMIT 1", (_call_lead_id,))
        _user_row = _user_cursor.fetchone()
        _call_org_id = _user_row.get('org_id') if _user_row else 1
        _user_conn.close()

        if _campaign_id:
            # Campaign-specific: load ONLY that campaign's product + persona + call flow
            from database import get_product_context_for_campaign
            _camp_product = get_product_context_for_campaign(_campaign_id)
            product_ctx = _camp_product.get("product_ctx", "")
            _product_persona = _camp_product.get("agent_persona", "")
            _product_call_flow = _camp_product.get("call_flow_instructions", "")
            _product_name = _camp_product.get("product_name", "")
            if not product_ctx:
                product_ctx = get_product_knowledge_context(org_id=_call_org_id)
        elif _call_org_id:
            custom = get_org_custom_prompt(_call_org_id)
            if custom.strip():
                product_ctx = "\n\n[PRODUCT KNOWLEDGE]:\n" + custom
            else:
                product_ctx = get_product_knowledge_context(org_id=_call_org_id)
        else:
            product_ctx = get_product_knowledge_context()
    except Exception:
        product_ctx = get_product_knowledge_context()

    # Build call context (voice identity, company name, lead source, system prompt)
    _ctx = build_call_context(
        lead_name=lead_name,
        lead_phone=lead_phone,
        interest=interest,
        _call_lead_id=_call_lead_id,
        _campaign_id=_campaign_id,
        _call_org_id=_call_org_id,
        _tts_voice_override=_tts_voice_override,
        product_ctx=product_ctx,
        _product_persona=_product_persona,
        _product_call_flow=_product_call_flow,
        pronunciation_ctx=pronunciation_ctx,
        _product_name=_product_name,
    )
    dynamic_context = _ctx["dynamic_context"]
    _agent_name = _ctx["_agent_name"]
    _lead_first = _ctx["_lead_first"]
    _company_name = _ctx["_company_name"]
    _bol = _ctx["_bol"]
    _source_context = _ctx["_source_context"]
    greeting_text = _ctx["greeting_text"]

    if not dg_client:
        dg_client = DeepgramClient(os.getenv("DEEPGRAM_API_KEY", "dummy"))
    if not llm_client:
        llm_client = genai.Client(api_key=(os.getenv("GEMINI_API_KEY") or "dummy").strip())

    dg_connection = dg_client.listen.websocket.v("1")
    loop = asyncio.get_event_loop()

    def on_error(self, error, **kwargs):
        logging.getLogger("uvicorn.error").error(f"[STT ERROR] Deepgram fired an error: {error}")

    def on_speech_started(self, **kwargs):
        if stream_sid:
            asyncio.run_coroutine_threadsafe(
                websocket.send_text(json.dumps({"event": "clear", "streamSid": stream_sid})),
                loop,
            )
        if stream_sid in active_tts_tasks and not active_tts_tasks[stream_sid].done():
            loop.call_soon_threadsafe(active_tts_tasks[stream_sid].cancel)

    def on_message(self, result, **kwargs):
        sentence = result.channel.alternatives[0].transcript
        if sentence and result.is_final:
            conv_logger = logging.getLogger("uvicorn.error")
            if _hangup_requested[0]:
                conv_logger.info(f"[STT] IGNORED (hangup pending): {sentence}")
                return
            conv_logger.info(f"[STT] USER SAID: {sentence}")
            if stream_sid:
                call_logger.call_event(stream_sid, "STT_TRANSCRIPT", sentence[:100])
            try:
                asyncio.run_coroutine_threadsafe(websocket.send_json({"event": "user_speech", "text": sentence}), loop)
            except Exception:
                pass
            chat_history.append({"role": "user", "parts": [{"text": sentence}]})

            async def _process_transcript():
                try:
                    t_start = time.time()
                    _last_transcript_time[0] = t_start
                    await asyncio.sleep(_debounce_delay)
                    if _last_transcript_time[0] != t_start:
                        logging.getLogger("uvicorn.error").info("[DEBOUNCE] Skipping older transcript — newer one pending.")
                        return
                    if _llm_lock.locked():
                        logging.getLogger("uvicorn.error").info("[TURN_GUARD] Skipping — LLM already processing.")
                        return

                    async with _llm_lock:
                        if stream_sid:
                            for monitor in monitor_connections.get(stream_sid, set()):
                                try:
                                    await monitor.send_json({"type": "transcript", "role": "user", "text": sentence})
                                except Exception:
                                    pass
                            if redis_store.get_takeover(stream_sid):
                                return
                            pending = redis_store.pop_all_whispers(stream_sid)
                            if pending:
                                for whisper in pending:
                                    chat_history.append({"role": "user", "parts": [{"text": f"Manager Whisper: {whisper}. Acknowledge this implicitly in your next response."}]})

                        # RAG via Local FAISS
                        rag_context = ""
                        if _call_org_id:
                            try:
                                import rag
                                context = rag.retrieve_context(sentence, _call_org_id, top_k=2)
                                if context:
                                    rag_context = "\n\n[COMPANY KNOWLEDGE - Check if this has facts relevant to the discussion and explicitly use it]:\n" + context
                            except Exception as e:
                                conv_logger.error(f"RAG FAISS lookup error: {e}")

                        t_pre_llm = time.time()
                        final_system_instruction = dynamic_context + rag_context

                        # Start TTS Worker Queue for Streaming Pipeline
                        tts_queue = asyncio.Queue()
                        
                        # [Phase 2: Conversational Backchanneling]
                        # Only inject a filler word if the user spoke a meaningful sentence (>2 words)
                        if len(sentence.split()) > 2:
                            import random
                            if random.random() < 0.6:  # 60% chance to trigger a human filler
                                fillers = ["Hmm...", "Achha...", "Okay...", "Haan..."]
                                await tts_queue.put(random.choice(fillers))
                        
                        async def tts_worker():
                            try:
                                while True:
                                    sentence = await tts_queue.get()
                                    if sentence is None:
                                        break
                                    await synthesize_and_send_audio(
                                        text=sentence, 
                                        stream_sid=stream_sid, 
                                        websocket=websocket, 
                                        tts_provider_override=_tts_provider_override, 
                                        tts_voice_override=_tts_voice_override, 
                                        tts_language_override=_tts_language_override
                                    )
                                    tts_queue.task_done()
                            except asyncio.CancelledError:
                                pass
                            except Exception as e:
                                conv_logger.error(f"TTS Worker Error: {e}")

                        if stream_sid in active_tts_tasks and not active_tts_tasks[stream_sid].done():
                            active_tts_tasks[stream_sid].cancel()
                            try:
                                await active_tts_tasks[stream_sid]
                            except (asyncio.CancelledError, Exception):
                                pass
                                
                        worker_task = asyncio.create_task(tts_worker())
                        if stream_sid:
                            active_tts_tasks[stream_sid] = worker_task

                        try:
                            import llm_provider
                            import re
                            
                            sentence_separators = re.compile(r'([.!?|\n]+)')
                            full_response = ""
                            current_sentence = ""
                            first_token_time = None
                            
                            async for chunk in llm_provider.generate_response_stream(
                                chat_history=chat_history,
                                system_instruction=final_system_instruction,
                                max_tokens=150,
                            ):
                                if first_token_time is None:
                                    first_token_time = time.time()
                                    conv_logger.info(f"TIMING: TTFB LLM = {first_token_time - t_pre_llm:.2f}s")
                                    
                                full_response += chunk
                                current_sentence += chunk
                                
                                parts = sentence_separators.split(current_sentence)
                                if len(parts) > 1:
                                    complete_text = "".join(parts[:-1]).strip()
                                    remaining_text = parts[-1]
                                    
                                    if complete_text:
                                        clean_text = re.sub(r'[\*\_\#\`\~\>\|]', '', complete_text)
                                        clean_text = re.sub(r'\[([^\]]+)\]\([^)]+\)', r'\1', clean_text)
                                        clean_text = clean_text.strip()
                                        if clean_text:
                                            await tts_queue.put(clean_text)
                                            
                                    current_sentence = remaining_text
                            
                            if current_sentence.strip():
                                clean_text = re.sub(r'[\*\_\#\`\~\>\|]', '', current_sentence.strip())
                                clean_text = re.sub(r'\[([^\]]+)\]\([^)]+\)', r'\1', clean_text)
                                clean_text = clean_text.strip()
                                if clean_text and "[HANGUP]" not in clean_text:
                                    await tts_queue.put(clean_text)
                                    
                            await tts_queue.put(None)

                            t_post_llm = time.time()
                            chat_history.append({"role": "model", "parts": [{"text": full_response}]})
                            conv_logger.info(f"[LLM] AI STREAM RESPONSE FULL: {full_response[:200]}")
                            conv_logger.info(f"TIMING: pre_llm={t_pre_llm - t_start:.2f}s, first_token={first_token_time - t_pre_llm if first_token_time else 0:.2f}s, total_gen={t_post_llm - t_pre_llm:.2f}s")
                            
                            try:
                                await websocket.send_json({"event": "llm_response", "text": full_response.replace("[HANGUP]", "")})
                            except Exception:
                                pass
                            if stream_sid:
                                call_logger.call_event(stream_sid, "LLM_RESPONSE", full_response[:100], llm_time_s=round(t_post_llm - t_pre_llm, 3))
                                for monitor in monitor_connections.get(stream_sid, set()):
                                    try:
                                        await monitor.send_json({"type": "transcript", "role": "agent", "text": full_response.replace("[HANGUP]", "")})
                                    except Exception:
                                        pass
                                        
                            # AI Physical Disconnect Command Handler
                            if "[HANGUP]" in full_response:
                                _hangup_requested[0] = True
                                conv_logger.info("[COMMAND] LLM explicitly commanded a websocket disconnect.")
                                if stream_sid:
                                    call_logger.call_event(stream_sid, "LLM_HANGUP", "AI explicitly ended the call block.")
                                # Wait for TTS worker to finish speaking ALL queued sentences
                                try:
                                    await asyncio.wait_for(worker_task, timeout=15)
                                    conv_logger.info("[HANGUP] TTS drain complete, closing connection.")
                                except (asyncio.TimeoutError, Exception):
                                    conv_logger.info("[HANGUP] TTS drain timed out, forcing close.")
                                # Grace period for browser to finish playing the audio buffer.
                                # TTS sends chunks faster than realtime — browser needs time to play them.
                                # ~4 seconds covers a typical farewell sentence at normal speech rate.
                                await asyncio.sleep(4)
                                try:
                                    await websocket.close()
                                except Exception:
                                    pass
                                return
                        except Exception as e:
                            import traceback
                            conv_logger.error(f"Error streaming LLM response: {e}")
                            conv_logger.error(traceback.format_exc())
                            await tts_queue.put(None)
                            return
                except Exception as _crash:
                    import traceback
                    logging.getLogger("uvicorn.error").error(f"[SYSTEM FATAL] _process_transcript SILENT CRASH: {_crash}")
                    logging.getLogger("uvicorn.error").error(traceback.format_exc())

            asyncio.run_coroutine_threadsafe(_process_transcript(), loop)

    dg_connection.on(LiveTranscriptionEvents.SpeechStarted, on_speech_started)
    dg_connection.on(LiveTranscriptionEvents.Transcript, on_message)

    dg_connection.start(
        LiveOptions(
            model="nova-2",
            language="hi",
            encoding="linear16",
            sample_rate=8000,
            channels=1,
            endpointing=200,          # Aggressive 200ms VAD endpointing
            utterance_end_ms="1000",  # Force utterance generation
            interim_results=True,     # Fix HTTP 400 deepgram crash
        )
    )

    ws_logger = logging.getLogger("uvicorn.error")
    ws_logger.info(f"Media stream handler started for {lead_name}")
    greeting_sent = False

    # CRITICAL: Send greeting immediately on WebSocket connect
    # Exotel VoiceBot has a 10-second timeout — if we don't send audio, it kills the session
    stream_sid = f"exotel-{_uuid.uuid4().hex[:12]}"
    is_exotel_stream = True
    twilio_websockets[stream_sid] = websocket
    monitor_connections[stream_sid] = set()
    redis_store.delete_whispers(stream_sid)
    redis_store.set_takeover(stream_sid, False)
    _tts_recording_buffers[stream_sid] = []
    ws_logger.info(f"[WS] Immediate stream init, sid={stream_sid}")

    chat_history.append({"role": "model", "parts": [{"text": greeting_text}]})
    ws_logger.info(f"[GREETING] Immediate greeting for {lead_name}, sid={stream_sid}")
    call_logger.call_event(stream_sid, "GREETING_SENT", f"to={lead_name}")
    greeting_sent = True
    active_tts_tasks[stream_sid] = asyncio.create_task(
        synthesize_and_send_audio(greeting_text, stream_sid, websocket, _tts_provider_override, _tts_voice_override, _tts_language_override)
    )

    try:
        while True:
            try:
                msg = await websocket.receive()
            except Exception as e:
                ws_logger.error(f"[WS RECV ERROR] Connection lost: {e}", exc_info=True)
                break

            if msg.get("type") == "websocket.disconnect":
                ws_logger.info(f"[WS DISCONNECT] Client sent disconnect frame, sid={stream_sid}")
                break

            # Handle binary frames (Exotel sends raw audio bytes)
            if "bytes" in msg and msg["bytes"]:
                audio_data = msg["bytes"]
                if len(_recording_mic_chunks) % 100 == 0:
                    ws_logger.info(f"[DEBUG-REC] Binary frame: {len(audio_data)} bytes, is_exotel={is_exotel_stream}, mic_chunks={len(_recording_mic_chunks)}")
                if not stream_sid:
                    stream_sid = f"exotel-{_uuid.uuid4().hex[:12]}"
                    twilio_websockets[stream_sid] = websocket
                    monitor_connections[stream_sid] = set()
                    redis_store.delete_whispers(stream_sid)
                    redis_store.set_takeover(stream_sid, False)
                    ws_logger.info(f"[WS] Exotel binary stream started, sid={stream_sid}")
                    call_logger.call_event(stream_sid, "WS_CONNECTED", f"name={lead_name}, phone={lead_phone}")
                    _tts_recording_buffers[stream_sid] = []

                if not greeting_sent:
                    greeting_sent = True
                    chat_history.append({"role": "model", "parts": [{"text": greeting_text}]})
                    ws_logger.info(f"[GREETING] Sending greeting for {lead_name}")
                    call_logger.call_event(stream_sid, "GREETING_SENT", f"to={lead_name}")
                    active_tts_tasks[stream_sid] = asyncio.create_task(
                        synthesize_and_send_audio(greeting_text, stream_sid, websocket, _tts_provider_override, _tts_voice_override, _tts_language_override)
                    )

                # Forward raw audio to Deepgram
                dg_connection.send(audio_data)
                # Capture mic audio for recording (mulaw→PCM)
                if is_exotel_stream:
                    import audioop as _ao
                    try:
                        pcm = _ao.ulaw2lin(audio_data, 2)
                        _recording_mic_chunks.append((time.time(), pcm))
                    except Exception:
                        pass

            # Handle text frames (Twilio/Exotel JSON)
            elif "text" in msg and msg["text"]:
                try:
                    data = json.loads(msg["text"])
                except Exception as e:
                    ws_logger.error(f"[WS JSON ERROR] Failed to parse msg: {e}", exc_info=True)
                    ws_logger.warning(f"Failed to parse WS text: {e}")
                    continue

                if data.get("event") != "media":
                    ws_logger.info(f"WS text message received: {str(data)[:200]}")

                if data.get("event") == "connected":
                    ws_logger.info("Exotel WebSocket connected event received")
                    continue
                elif data.get("event") == "start":
                    stream_sid = (
                        data.get("stream_sid")
                        or data.get("start", {}).get("streamSid")
                        or f"exotel-{_uuid.uuid4().hex[:12]}"
                    )
                    if stream_sid.startswith("web_sim_"):
                        is_exotel_stream = False
                        ws_logger.info(f"[BROWSER SIM] Detected web simulator stream, sid={stream_sid}")
                    else:
                        is_exotel_stream = True
                        ws_logger.info(f"[EXOTEL] Detected Exotel stream, sid={stream_sid}")
                    ws_logger.info(f"[DEBUG-REC] Stream started: sid={stream_sid}, exotel={is_exotel_stream}, start_data_keys={list(data.keys())}")
                    
                    # [RACE CONDITION FIX] Map strict CallSid from Exotel payload
                    call_sid = data.get("start", {}).get("callSid") or data.get("call_sid") or data.get("CallSid")
                    info = redis_store.get_pending_call(call_sid) if call_sid else {}
                    if info:
                        true_name = info.get("name", "Customer")
                        old_name = lead_name
                        lead_name = true_name
                        _call_lead_id = info.get("lead_id", _call_lead_id)
                        _exotel_call_sid = call_sid
                        dynamic_context = dynamic_context.replace(f"Tum {old_name} ko call kar rahe ho", f"Tum {true_name} ko call kar rahe ho")
                        ws_logger.info(f"[CONTEXT FIX] Successfully mapped CallSid {call_sid} to {true_name}, LeadID: {_call_lead_id}")

                    twilio_websockets[stream_sid] = websocket
                    monitor_connections[stream_sid] = set()
                    redis_store.delete_whispers(stream_sid)
                    redis_store.set_takeover(stream_sid, False)
                    _tts_recording_buffers[stream_sid] = []

                    if not greeting_sent:
                        greeting_sent = True
                        ws_logger.info(f"GREETING: Triggering TTS greeting for stream {stream_sid}")
                        active_tts_tasks[stream_sid] = asyncio.create_task(
                            synthesize_and_send_audio(
                                f"नमस्ते {_lead_first} जी, मैं {_agent_name}, {_company_name} से {_bol}। आपने {_source_context} क्या?",
                                stream_sid, websocket, _tts_provider_override, _tts_voice_override, _tts_language_override,
                            )
                        )
                elif data.get("event") == "media":
                    raw_audio = base64.b64decode(data["media"]["payload"])
                    dg_connection.send(raw_audio)
                    if len(_recording_mic_chunks) % 100 == 0:
                        ws_logger.info(f"[DEBUG-REC] Media event: {len(raw_audio)} bytes, is_exotel={is_exotel_stream}, mic_chunks={len(_recording_mic_chunks)}")
                    if is_exotel_stream:
                        import audioop as _ao2
                        try:
                            pcm = _ao2.ulaw2lin(raw_audio, 2)
                            _recording_mic_chunks.append((time.time(), pcm))
                        except Exception as _mic_err:
                            ws_logger.error(f"[DEBUG-REC] Mic capture error: {_mic_err}")
                    else:
                        # Capture anyway for non-exotel streams (browser sim sends PCM directly)
                        _recording_mic_chunks.append((time.time(), raw_audio))
                elif data.get("event") == "stop":
                    print("Media stream stopped.")
                    break
                else:
                    if not stream_sid:
                        stream_sid = f"exotel-{_uuid.uuid4().hex[:12]}"
                        twilio_websockets[stream_sid] = websocket
                        monitor_connections[stream_sid] = set()
                        redis_store.delete_whispers(stream_sid)
                        redis_store.set_takeover(stream_sid, False)
                        ws_logger.info(f"Exotel text stream started, sid={stream_sid}")
                    _tts_recording_buffers.setdefault(stream_sid, [])
                    if not greeting_sent:
                        greeting_sent = True
                        active_tts_tasks[stream_sid] = asyncio.create_task(
                            synthesize_and_send_audio(
                                f"नमस्ते {_lead_first} जी, मैं {_agent_name}, {_company_name} से {_bol}। आपने {_source_context} क्या?",
                                stream_sid, websocket, _tts_provider_override, _tts_voice_override, _tts_language_override,
                            )
                        )
    except Exception as e:
        logging.getLogger("uvicorn.error").error(f"[WS] Error in media stream: {e}")
        if stream_sid:
            call_logger.call_event(stream_sid, "WS_ERROR", str(e))
    finally:
        logging.getLogger("uvicorn.error").info(f"[WS CLOSED] sid={stream_sid}, turns={len(chat_history)}, exotel={is_exotel_stream}")
        
        # Cleanup any active TTS tasks to prevent dangling background processes
        if stream_sid in active_tts_tasks:
            t = active_tts_tasks[stream_sid]
            if not t.done():
                t.cancel()
            del active_tts_tasks[stream_sid]
            
        if stream_sid:
            call_logger.call_event(stream_sid, "WS_DISCONNECTED", f"turns={len(chat_history)}")
            call_logger.end_call(stream_sid)
            # Save transcript and recording to DB
            if _call_lead_id and chat_history:
                try:
                    await save_call_recording_and_transcript(
                        stream_sid=stream_sid,
                        _call_lead_id=_call_lead_id,
                        _exotel_call_sid=_exotel_call_sid,
                        chat_history=chat_history,
                        _recording_mic_chunks=_recording_mic_chunks,
                        _tts_recording_buffers=_tts_recording_buffers,
                        _call_start_time=_call_start_time,
                        EXOTEL_API_KEY=EXOTEL_API_KEY,
                        EXOTEL_API_TOKEN=EXOTEL_API_TOKEN,
                        EXOTEL_ACCOUNT_SID=EXOTEL_ACCOUNT_SID,
                        _campaign_id=_campaign_id,
                    )
                except Exception as _te:
                    import traceback
                    ws_logger.error(f"[TRANSCRIPT] Error saving: {_te}\n{traceback.format_exc()}")

        # Cleanup
        if stream_sid:
            redis_store.cleanup_call(stream_sid)
        if stream_sid and stream_sid in _tts_recording_buffers:
            del _tts_recording_buffers[stream_sid]
        if stream_sid and stream_sid in twilio_websockets:
            del twilio_websockets[stream_sid]
        try:
            dg_connection.finish()
        except Exception:
            pass
        try:
            await websocket.close()
        except Exception:
            pass

        # Omnichannel Summary & WhatsApp Trigger
        if len(chat_history) > 2:
            try:
                transcript_text = "\n".join([f"{m['role']}: {m['parts'][0]['text']}" for m in chat_history if isinstance(m, dict) and 'parts' in m])
                summary_prompt = "You are a sales evaluator. Analyze the transcript. Return strictly a valid JSON object with: {'sentiment': 'Cold/Warm/Hot', 'requires_brochure': true/false, 'note': 'short summary of next steps'}. If the lead asks for details, pricing, or a brochure, set requires_brochure to true."
                res = await llm_client.aio.models.generate_content(
                    model="gemini-2.5-flash",
                    contents=transcript_text,
                    config=types.GenerateContentConfig(system_instruction=summary_prompt)
                )
                text = res.text.replace("```json", "").replace("```", "").strip()
                outcome = json.loads(text)
                if lead_phone:
                    from database import update_call_note
                    update_call_note("ws_" + str(stream_sid), outcome.get("note", "Call completed via Dialer."), lead_phone)
            except Exception as e:
                print(f"Omnichannel intent trigger error: {e}")


# ─── Sandbox Stream ─────────────────────────────────────────────────────────

async def sandbox_stream(websocket: WebSocket):
    await websocket.accept()
    from deepgram import DeepgramClient, LiveTranscriptionEvents, LiveOptions
    import os, json, base64
    import llm_provider
    import httpx
    
    dg = DeepgramClient(os.getenv("DEEPGRAM_API_KEY", "dummy"))
    dg_conn = dg.listen.websocket.v("1")
    chat_hist = []

    async def on_message(self, result, **kwargs):
        sentence = result.channel.alternatives[0].transcript
        if sentence and result.is_final:
            chat_hist.append({"role": "user", "parts": [{"text": sentence}]})
            await websocket.send_json({"type": "transcript", "role": "user", "text": sentence})
            try:
                system_prompt = "You are in AI sandbox test mode. A sales manager is interacting with you. Be extremely aggressive answering sales objections, keeping answers to one line."
                response_text = await llm_provider.generate_response(
                    chat_history=chat_hist,
                    system_instruction=system_prompt,
                    max_tokens=150
                )
                
                chat_hist.append({"role": "model", "parts": [{"text": response_text}]})
                
                url = f"https://api.elevenlabs.io/v1/text-to-speech/{os.getenv('ELEVENLABS_VOICE_ID')}/stream?output_format=mp3_44100_128"
                headers = {"xi-api-key": os.getenv("ELEVENLABS_API_KEY")}
                payload = {"text": response_text, "model_id": "eleven_turbo_v2"}
                async with httpx.AsyncClient() as client:
                    async with client.stream("POST", url, json=payload, headers=headers) as resp:
                        async for chunk in resp.aiter_bytes(chunk_size=4000):
                            if chunk:
                                await websocket.send_json({"type": "audio", "payload": base64.b64encode(chunk).decode('utf-8')})
                                
                await websocket.send_json({"type": "transcript", "role": "agent", "text": response_text})
            except Exception as e:
                import logging
                logging.getLogger("uvicorn.error").error(f"[SANDBOX CRASH] LLM Provider Error: {e}", exc_info=True)
                print(f"Sandbox LLM Error: {e}")

    dg_conn.on(LiveTranscriptionEvents.Transcript, on_message)
    await dg_conn.start(LiveOptions(
        model="nova-2", language="en-US", encoding="linear16", sample_rate=16000, channels=1, endpointing=True
    ))

    try:
        while True:
            data = await websocket.receive_json()
            if data.get("type") == "audio_chunk":
                raw_bytes = base64.b64decode(data["payload"])
                await dg_conn.send(raw_bytes)
    except Exception:
        pass
    finally:
        await dg_conn.finish()
        await websocket.close()


# ─── Monitor / Whisper Stream ───────────────────────────────────────────────

async def monitor_call(websocket: WebSocket, stream_sid: str):
    await websocket.accept()
    if stream_sid not in monitor_connections:
        monitor_connections[stream_sid] = set()
    monitor_connections[stream_sid].add(websocket)

    try:
        while True:
            data = await websocket.receive_json()
            if data.get("action") == "whisper":
                redis_store.push_whisper(stream_sid, data.get("text", ""))
            elif data.get("action") == "takeover":
                redis_store.set_takeover(stream_sid, True)
                if stream_sid in active_tts_tasks and not active_tts_tasks[stream_sid].done():
                    active_tts_tasks[stream_sid].cancel()
            elif data.get("action") == "audio_chunk" and redis_store.get_takeover(stream_sid):
                target_ws = twilio_websockets.get(stream_sid)
                if target_ws:
                    await target_ws.send_text(json.dumps({
                        "event": "media",
                        "streamSid": stream_sid,
                        "media": {"payload": data.get("payload")}
                    }))
    except Exception:
        pass
    finally:
        if stream_sid in monitor_connections and websocket in monitor_connections[stream_sid]:
            monitor_connections[stream_sid].remove(websocket)
