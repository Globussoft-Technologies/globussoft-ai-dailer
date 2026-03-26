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

# ─── Shared State ────────────────────────────────────────────────────────────

active_tts_tasks = {}
monitor_connections: dict[str, set[WebSocket]] = {}
whisper_queues: dict[str, list[str]] = {}
takeover_active: dict[str, bool] = {}
twilio_websockets: dict[str, WebSocket] = {}
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
    _qp_lead_id = websocket.query_params.get("lead_id", "")
    if _qp_lead_id and _qp_lead_id.isdigit():
        _call_lead_id = int(_qp_lead_id)
    _tts_provider_override = websocket.query_params.get("tts_provider", None) or None
    _tts_voice_override = websocket.query_params.get("voice", None) or None
    _tts_language_override = websocket.query_params.get("tts_language", None) or None

    # If no voice override passed, look up org voice settings from DB
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
        info = pending_call_info.get("latest", {})
        lead_name = info.get("name", "Customer")
        interest = info.get("interest", "our platform") if not interest else interest
        lead_phone = info.get("phone", "") if not lead_phone else lead_phone
        if not _call_lead_id:
            _call_lead_id = info.get("lead_id")

    EXOTEL_API_KEY = (os.getenv("EXOTEL_API_KEY") or "").strip()
    EXOTEL_API_TOKEN = (os.getenv("EXOTEL_API_TOKEN") or "").strip()
    EXOTEL_ACCOUNT_SID = (os.getenv("EXOTEL_ACCOUNT_SID") or "").strip()

    _exotel_call_sid = (pending_call_info.get("latest", {}).get("exotel_call_sid") or "")
    _call_start_time = time.time()
    stream_sid = None
    is_exotel_stream = False
    chat_history = []
    _llm_lock = asyncio.Lock()
    _last_transcript_time = [0.0]
    _debounce_delay = 0.4
    _recording_mic_chunks = []
    _recording_tts_chunks = []

    # Load pronunciation guide
    pronunciation_ctx = get_pronunciation_context()

    # Load product knowledge for system prompt
    product_ctx = ""
    try:
        _user_conn = get_conn()
        _user_cursor = _user_conn.cursor()
        _user_cursor.execute("SELECT u.org_id FROM users u JOIN leads l ON 1=1 WHERE l.id = %s LIMIT 1", (_call_lead_id,))
        _user_row = _user_cursor.fetchone()
        _call_org_id = _user_row.get('org_id') if _user_row else None
        _user_conn.close()
        if _call_org_id:
            custom = get_org_custom_prompt(_call_org_id)
            if custom.strip():
                product_ctx = "\n\n[PRODUCT KNOWLEDGE]:\n" + custom
            else:
                product_ctx = get_product_knowledge_context(org_id=_call_org_id)
        else:
            product_ctx = get_product_knowledge_context()
    except Exception:
        product_ctx = get_product_knowledge_context()

    dynamic_context = (
        f"Tum Arjun ho — ek friendly, professional lead qualifier. Tum {lead_name} ko call kar rahe ho. "
        f"Tumhare records mein hai ki unhone {interest} ke baare mein ek form bhara tha website par. "
        f"\n\nTUMHARA ROLE: Tum lead qualify karne ke liye call kar rahe ho aur product ke baare mein briefly bata sakte ho. "
        f"Tumhara kaam hai: "
        f"(a) Confirm karo ki unhone sach mein form bhara tha ya nahi. "
        f"(b) Agar haan, toh puchho ki unhe abhi bhi interest hai ya nahi. "
        f"(c) Agar user PRODUCT ke baare mein puchhe ('kya hai ye?', 'ye kya karta hai?'), toh PRODUCT KNOWLEDGE section se "
        f"    1-2 line mein briefly batao ki product kya hai aur kya karta hai. PHIR bolo ki 'isse detail mein humara senior representative samjhayega, "
        f"    main aapke liye ek meeting schedule kar deta hoon.' "
        f"(d) Agar interest hai, toh ek appointment/callback schedule karo — puchho ki kab free hain, "
        f"    aur bolo ki humara senior representative unhe us time call karega. "
        f"\n\nCONVERSATION RULES: "
        f"1. SUNNA PEHLE: User jo bole, pehle samjho, phir respond karo. User ki baat ignore mat karo. "
        f"2. FORM DENIAL: Agar user bole 'maine form nahi bhara', 'yaad nahi', 'nahi kiya' — toh MAAN JAO. "
        f"   Bolo: 'achha sorry, ho sakta hai galti se number aa gaya ho, aapko disturb karne ke liye maafi, aapka din acha ho.' Conversation politely end karo. "
        f"3. NO INTEREST: Agar user bole 'interest nahi hai', 'nahi chahiye' — respectfully bolo 'bilkul samajh gaya, koi baat nahi, thank you for your time.' End karo. "
        f"4. INTERESTED: Agar user interested hai, toh puchho 'achha great, toh aap kab free honge? Humara representative aapko detail mein baat karega.' "
        f"   Unka preferred time le lo aur bolo ki call scheduled hai. "
        f"5. SHORT RESPONSES: 1-2 sentences max. Phone pe chhota bolo, jaise real person baat karta hai. "
        f"IMPORTANT LANGUAGE RULE: SIRF ROMAN HINDI/ENGLISH likhna hai. KABHI BHI Devanagari script mat use karo. "
        f"Hamesha Roman letters mein likho jaise: Namaste, Achha, Bilkul. "
        f"6. LANGUAGE: Natural Hindi-Hinglish. 'haan', 'bilkul', 'achha', 'zaroor' use karo. "
        f"7. NO FORMATTING: Kabhi markdown, asterisks, bold, bullets mat use karo. "
        f"8. IDENTITY: Tum Arjun ho, ek human. Kabhi mat bolo ki tum AI ho. "
        f"9. NO REPETITION: Jo bol chuke ho dubara mat bolo. Conversation aage le jaao. "
        f"10. NATURAL FLOW: Agar user ne sirf 'achha' ya 'haan' bola toh features mat bolo — puchho 'toh aapko abhi bhi interest hai? Agar haan toh main ek appointment schedule kar deta hoon.'"
        f"{pronunciation_ctx}"
        f"{product_ctx}"
    )

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
                            if takeover_active.get(stream_sid, False):
                                return
                            pending = whisper_queues.get(stream_sid, [])
                            if pending:
                                for whisper in pending:
                                    chat_history.append({"role": "user", "parts": [{"text": f"Manager Whisper: {whisper}. Acknowledge this implicitly in your next response."}]})
                                pending.clear()

                        # RAG
                        rag_context = ""
                        try:
                            from routes import knowledge_collection
                            if knowledge_collection and knowledge_collection.count() > 0:
                                import google.generativeai as gai
                                gai.configure(api_key=os.getenv("GEMINI_API_KEY", "dummy"))
                                _loop = asyncio.get_event_loop()
                                res = await _loop.run_in_executor(
                                    None,
                                    lambda: gai.embed_content(model="models/text-embedding-004", content=sentence, task_type="retrieval_query")
                                )
                                query_emb = res['embedding']
                                results = knowledge_collection.query(query_embeddings=[query_emb], n_results=2)
                                if results and results.get('documents') and results['documents'][0]:
                                    docs = results['documents'][0]
                                    rag_context = "\n[KNOWLEDGE BASE RELEVANT INFO]:\n" + "\n---\n".join(docs)
                        except Exception as e:
                            print(f"RAG error: {e}")

                        t_pre_llm = time.time()
                        final_system_instruction = dynamic_context + rag_context

                        try:
                            import llm_provider
                            response_text = await llm_provider.generate_response(
                                chat_history=chat_history,
                                system_instruction=final_system_instruction,
                                max_tokens=150,
                            )
                            t_post_llm = time.time()
                            chat_history.append({"role": "model", "parts": [{"text": response_text}]})
                            conv_logger.info(f"[LLM] AI RESPONSE: {response_text[:200]}")
                            try:
                                await websocket.send_json({"event": "llm_response", "text": response_text})
                            except Exception:
                                pass
                            if stream_sid:
                                call_logger.call_event(stream_sid, "LLM_RESPONSE", response_text[:100], llm_time_s=round(t_post_llm - t_pre_llm, 3))
                                for monitor in monitor_connections.get(stream_sid, set()):
                                    try:
                                        await monitor.send_json({"type": "transcript", "role": "agent", "text": response_text})
                                    except Exception:
                                        pass
                        except Exception as e:
                            import traceback
                            conv_logger.error(f"Error fetching LLM response: {e}")
                            conv_logger.error(traceback.format_exc())
                            return

                        if stream_sid:
                            import re
                            clean_text = re.sub(r'[\*\_\#\`\~\>\|]', '', response_text)
                            clean_text = re.sub(r'\[([^\]]+)\]\([^)]+\)', r'\1', clean_text)
                            clean_text = clean_text.strip()
                            conv_logger.info(f"TIMING: pre_llm={t_pre_llm - t_start:.2f}s, llm={t_post_llm - t_pre_llm:.2f}s, total_to_tts={time.time() - t_start:.2f}s")
                            if stream_sid in active_tts_tasks and not active_tts_tasks[stream_sid].done():
                                active_tts_tasks[stream_sid].cancel()
                                try:
                                    await active_tts_tasks[stream_sid]
                                except (asyncio.CancelledError, Exception):
                                    pass
                            active_tts_tasks[stream_sid] = asyncio.create_task(
                                synthesize_and_send_audio(clean_text, stream_sid, websocket, _tts_provider_override, _tts_voice_override, _tts_language_override)
                            )
                except Exception as _crash:
                    import traceback
                    logging.getLogger("uvicorn.error").error(f"[SYSTEM FATAL] _process_transcript SILENT CRASH: {_crash}")
                    logging.getLogger("uvicorn.error").error(traceback.format_exc())

            asyncio.run_coroutine_threadsafe(_process_transcript(), loop)

    dg_connection.on(LiveTranscriptionEvents.SpeechStarted, on_speech_started)
    dg_connection.on(LiveTranscriptionEvents.Transcript, on_message)

    dg_connection.start(
        LiveOptions(
            model="nova-3",
            language="hi",
            encoding="linear16",
            sample_rate=8000,
            channels=1,
            endpointing=300,
            interim_results=True,
            utterance_end_ms=1000,
            vad_events=True,
        )
    )

    ws_logger = logging.getLogger("uvicorn.error")
    ws_logger.info(f"Media stream handler started for {lead_name}")
    greeting_sent = False

    try:
        while True:
            try:
                msg = await websocket.receive()
            except Exception as e:
                ws_logger.error(f"[WS RECV ERROR] Connection lost: {e}")
                break

            if msg.get("type") == "websocket.disconnect":
                ws_logger.info(f"[WS DISCONNECT] Client sent disconnect frame, sid={stream_sid}")
                break

            # Handle binary frames (Exotel sends raw audio bytes)
            if "bytes" in msg and msg["bytes"]:
                audio_data = msg["bytes"]
                if not stream_sid:
                    stream_sid = f"exotel-{_uuid.uuid4().hex[:12]}"
                    twilio_websockets[stream_sid] = websocket
                    monitor_connections[stream_sid] = set()
                    whisper_queues[stream_sid] = []
                    takeover_active[stream_sid] = False
                    ws_logger.info(f"[WS] Exotel binary stream started, sid={stream_sid}")
                    call_logger.call_event(stream_sid, "WS_CONNECTED", f"name={lead_name}, phone={lead_phone}")
                    _tts_recording_buffers[stream_sid] = []

                if not greeting_sent:
                    greeting_sent = True
                    greeting_text = f"हैलो {lead_name} जी? मैं AdsGPT से बात कर रहा हूँ, आपने हमारे AI platform के regarding enquiry की थी?"
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
                    elif data.get("stream_sid"):
                        is_exotel_stream = True
                    ws_logger.info(f"Stream started: sid={stream_sid}, exotel={is_exotel_stream}")
                    twilio_websockets[stream_sid] = websocket
                    monitor_connections[stream_sid] = set()
                    whisper_queues[stream_sid] = []
                    takeover_active[stream_sid] = False
                    _tts_recording_buffers[stream_sid] = []

                    if not greeting_sent:
                        greeting_sent = True
                        ws_logger.info(f"GREETING: Triggering TTS greeting for stream {stream_sid}")
                        active_tts_tasks[stream_sid] = asyncio.create_task(
                            synthesize_and_send_audio(
                                f"हैलो {lead_name} जी? मैं AdsGPT से बात कर रहा हूँ, आपने हमारे AI platform के regarding enquiry की थी?",
                                stream_sid, websocket, _tts_provider_override, _tts_voice_override, _tts_language_override,
                            )
                        )
                elif data.get("event") == "media":
                    raw_audio = base64.b64decode(data["media"]["payload"])
                    dg_connection.send(raw_audio)
                    if is_exotel_stream:
                        import audioop as _ao2
                        try:
                            pcm = _ao2.ulaw2lin(raw_audio, 2)
                            _recording_mic_chunks.append((time.time(), pcm))
                        except Exception:
                            pass
                elif data.get("event") == "stop":
                    print("Media stream stopped.")
                    break
                else:
                    if not stream_sid:
                        stream_sid = f"exotel-{_uuid.uuid4().hex[:12]}"
                        twilio_websockets[stream_sid] = websocket
                        monitor_connections[stream_sid] = set()
                        whisper_queues[stream_sid] = []
                        takeover_active[stream_sid] = False
                        ws_logger.info(f"Exotel text stream started, sid={stream_sid}")
                    _tts_recording_buffers.setdefault(stream_sid, [])
                    if not greeting_sent:
                        greeting_sent = True
                        active_tts_tasks[stream_sid] = asyncio.create_task(
                            synthesize_and_send_audio(
                                f"हैलो {lead_name} जी? मैं AdsGPT से बात कर रहा हूँ, आपने हमारे AI platform के regarding enquiry की थी?",
                                stream_sid, websocket, _tts_provider_override, _tts_voice_override, _tts_language_override,
                            )
                        )
    except Exception as e:
        logging.getLogger("uvicorn.error").error(f"[WS] Error in media stream: {e}")
        if stream_sid:
            call_logger.call_event(stream_sid, "WS_ERROR", str(e))
    finally:
        logging.getLogger("uvicorn.error").info(f"[WS CLOSED] sid={stream_sid}, turns={len(chat_history)}, exotel={is_exotel_stream}")
        if stream_sid:
            call_logger.call_event(stream_sid, "WS_DISCONNECTED", f"turns={len(chat_history)}")
            call_logger.end_call(stream_sid)
            # Save transcript to DB
            if _call_lead_id and chat_history:
                try:
                    transcript_turns = []
                    for msg in chat_history:
                        role = "AI" if msg.get("role") == "model" else "User"
                        text = ""
                        parts = msg.get("parts", [])
                        if parts and isinstance(parts[0], dict):
                            text = parts[0].get("text", "")
                        elif parts and isinstance(parts[0], str):
                            text = parts[0]
                        if text:
                            transcript_turns.append({"role": role, "text": text})

                    recording_url = None
                    if _exotel_call_sid:
                        try:
                            ws_logger.info(f"[RECORDING] Fetching for SID: {_exotel_call_sid}")
                            creds = f"{EXOTEL_API_KEY}:{EXOTEL_API_TOKEN}"
                            auth_b64 = base64.b64encode(creds.encode()).decode()
                            # Exotel recording endpoint — append .json to avoid XML response
                            rec_api_url = f"https://api.exotel.com/v1/Accounts/{EXOTEL_ACCOUNT_SID}/Calls/{_exotel_call_sid}/Recording.json"
                            async with httpx.AsyncClient(timeout=30.0, follow_redirects=True) as _hc:
                                rec_resp = await _hc.get(rec_api_url, headers={"Authorization": f"Basic {auth_b64}"})
                            if rec_resp.status_code == 200:
                                content_type = rec_resp.headers.get("content-type", "")
                                ws_logger.info(f"[RECORDING] Response content-type: {content_type}, size: {len(rec_resp.content)} bytes")
                                if "json" in content_type or "text" in content_type or "xml" in content_type:
                                    # JSON response — extract RecordingUrl and download it
                                    try:
                                        rec_data = rec_resp.json()
                                        # Exotel sometimes wraps in "Call" or "Recording"
                                        call_obj = rec_data.get("Call", {})
                                        remote_url = (
                                            rec_data.get("Recording", {}).get("RecordingUrl") or 
                                            rec_data.get("RecordingUrl") or
                                            call_obj.get("RecordingUrl")
                                        )
                                        if not remote_url:
                                            ws_logger.info(f"[RECORDING] Missing URL initially, waiting 10s for Exotel transcoding...")
                                            await asyncio.sleep(10)
                                            rec_resp2 = await _hc.get(rec_api_url, headers={"Authorization": f"Basic {auth_b64}"})
                                            if rec_resp2.status_code == 200:
                                                rec_data2 = rec_resp2.json()
                                                call_obj2 = rec_data2.get("Call", {})
                                                remote_url = (
                                                    rec_data2.get("Recording", {}).get("RecordingUrl") or 
                                                    rec_data2.get("RecordingUrl") or
                                                    call_obj2.get("RecordingUrl")
                                                )
                                        
                                        if remote_url:
                                            ws_logger.info(f"[RECORDING] Got remote URL: {remote_url}, downloading...")
                                            async with httpx.AsyncClient(timeout=30.0, follow_redirects=True) as _hc2:
                                                audio_resp = await _hc2.get(remote_url, headers={"Authorization": f"Basic {auth_b64}"})
                                            if audio_resp.status_code == 200 and len(audio_resp.content) > 1000:
                                                _rec_dir = os.path.join(os.path.dirname(__file__), "recordings")
                                                os.makedirs(_rec_dir, exist_ok=True)
                                                ext = "mp3"  # Exotel typically serves MP3
                                                if "wav" in audio_resp.headers.get("content-type", ""):
                                                    ext = "wav"
                                                _rec_fname = f"call_{_call_lead_id}_{int(_call_start_time)}.{ext}"
                                                _rec_path = os.path.join(_rec_dir, _rec_fname)
                                                with open(_rec_path, "wb") as f:
                                                    f.write(audio_resp.content)
                                                recording_url = f"/api/recordings/{_rec_fname}"
                                                ws_logger.info(f"[RECORDING] Downloaded and saved: {_rec_path} ({len(audio_resp.content)} bytes)")
                                            else:
                                                ws_logger.warning(f"[RECORDING] Failed to download from {remote_url}, status={audio_resp.status_code}, len={len(audio_resp.content)}")
                                        else:
                                            ws_logger.warning(f"[RECORDING] Missing RecordingUrl even after retry. JSON payload: {rec_data}")
                                    except Exception as _je:
                                        ws_logger.error(f"[RECORDING] JSON parse or download error: {_je}")
                                elif len(rec_resp.content) > 1000:
                                    # Direct audio bytes returned (not JSON)
                                    _rec_dir = os.path.join(os.path.dirname(__file__), "recordings")
                                    os.makedirs(_rec_dir, exist_ok=True)
                                    ext = "mp3"
                                    if "wav" in content_type:
                                        ext = "wav"
                                    elif "ogg" in content_type:
                                        ext = "ogg"
                                    _rec_fname = f"call_{_call_lead_id}_{int(_call_start_time)}.{ext}"
                                    _rec_path = os.path.join(_rec_dir, _rec_fname)
                                    with open(_rec_path, "wb") as f:
                                        f.write(rec_resp.content)
                                    recording_url = f"/api/recordings/{_rec_fname}"
                                    ws_logger.info(f"[RECORDING] Saved direct audio: {_rec_path} ({len(rec_resp.content)} bytes)")
                                else:
                                    ws_logger.warning(f"[RECORDING] Response too small ({len(rec_resp.content)} bytes), skipping")
                            else:
                                ws_logger.warning(f"[RECORDING] Exotel returned {rec_resp.status_code}")
                        except Exception as _re:
                            ws_logger.error(f"[RECORDING] Error fetching: {_re}")

                    call_duration = round(time.time() - _call_start_time, 1)
                    if transcript_turns:
                        save_call_transcript(
                            lead_id=_call_lead_id,
                            transcript_json=json.dumps(transcript_turns, ensure_ascii=False),
                            recording_url=recording_url,
                            call_duration_s=call_duration
                        )
                except Exception as _te:
                    import traceback
                    ws_logger.error(f"[TRANSCRIPT] Error saving: {_te}\n{traceback.format_exc()}")

        # Cleanup
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
    dg = DeepgramClient(os.getenv("DEEPGRAM_API_KEY", "dummy"))
    dg_conn = dg.listen.websocket.v("1")
    llm = genai.Client(api_key=os.getenv("GEMINI_API_KEY", "dummy"))
    chat_hist = []

    async def on_message(self, result, **kwargs):
        sentence = result.channel.alternatives[0].transcript
        if sentence and result.is_final:
            chat_hist.append({"role": "user", "parts": [{"text": sentence}]})
            await websocket.send_json({"type": "transcript", "role": "user", "text": sentence})
            try:
                response = await llm.aio.models.generate_content(
                    model="gemini-2.5-flash",
                    contents=chat_hist,
                    config=types.GenerateContentConfig(system_instruction="You are in AI sandbox test mode. A sales manager is interacting with you. Be extremely aggressive answering sales objections, keeping answers to one line.")
                )
                chat_hist.append({"role": "model", "parts": [{"text": response.text}]})
                url = f"https://api.elevenlabs.io/v1/text-to-speech/{os.getenv('ELEVENLABS_VOICE_ID')}/stream?output_format=mp3_44100_128"
                headers = {"xi-api-key": os.getenv("ELEVENLABS_API_KEY")}
                payload = {"text": response.text, "model_id": "eleven_turbo_v2"}
                async with httpx.AsyncClient() as client:
                    async with client.stream("POST", url, json=payload, headers=headers) as resp:
                        async for chunk in resp.aiter_bytes(chunk_size=4000):
                            if chunk:
                                await websocket.send_json({"type": "audio", "payload": base64.b64encode(chunk).decode('utf-8')})
                await websocket.send_json({"type": "transcript", "role": "agent", "text": response.text})
            except Exception:
                pass

    dg_conn.on(LiveTranscriptionEvents.Transcript, on_message)
    await dg_conn.start(LiveOptions(
        model="nova-3", language="en-US", encoding="linear16", sample_rate=16000, channels=1, endpointing=True
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
                q = whisper_queues.setdefault(stream_sid, [])
                q.append(data.get("text", ""))
            elif data.get("action") == "takeover":
                takeover_active[stream_sid] = True
                if stream_sid in active_tts_tasks and not active_tts_tasks[stream_sid].done():
                    active_tts_tasks[stream_sid].cancel()
            elif data.get("action") == "audio_chunk" and takeover_active.get(stream_sid, False):
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
