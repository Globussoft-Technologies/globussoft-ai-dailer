"""
recording_service.py — Handles call recording assembly and transcript persistence
for the AI Dialer.
"""
import os
import json
import base64
import asyncio
import time
import logging
import httpx

from database import save_call_transcript, save_call_review, create_retry, has_pending_or_exhausted_retry, get_conn, get_lead_by_id, add_dnd_number
from webhook_dispatch import dispatch_webhook


async def save_call_recording_and_transcript(
    stream_sid,
    _call_lead_id,
    _exotel_call_sid,
    chat_history,
    _recording_mic_chunks,
    _tts_recording_buffers,
    _call_start_time,
    EXOTEL_API_KEY,
    EXOTEL_API_TOKEN,
    EXOTEL_ACCOUNT_SID,
    _campaign_id=None,
):
    """
    Save the call transcript to the DB, fetch the Exotel recording (with retries),
    and fall back to server-side stereo WAV merge when Exotel recording is unavailable.
    """
    ws_logger = logging.getLogger("uvicorn.error")

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

    # --- Exotel recording fetch with retries ---
    if _exotel_call_sid:
        try:
            ws_logger.info(f"[RECORDING] Fetching for SID: {_exotel_call_sid}")
            creds = f"{EXOTEL_API_KEY}:{EXOTEL_API_TOKEN}"
            auth_b64 = base64.b64encode(creds.encode()).decode()
            rec_api_url = f"https://api.exotel.com/v1/Accounts/{EXOTEL_ACCOUNT_SID}/Calls/{_exotel_call_sid}/Recording.json"

            rec_resp = None
            for _attempt in range(6):
                async with httpx.AsyncClient(timeout=30.0, follow_redirects=True) as _hc:
                    rec_resp = await _hc.get(rec_api_url, headers={"Authorization": f"Basic {auth_b64}"})

                if rec_resp.status_code != 200:
                    ws_logger.warning(f"[RECORDING] Exotel returned {rec_resp.status_code}, attempt {_attempt+1}")
                    await asyncio.sleep(10)
                    continue

                content_type = rec_resp.headers.get("content-type", "")

                # Check if direct audio bytes
                if "audio" in content_type and len(rec_resp.content) > 1000:
                    _rec_dir = os.path.join(os.path.dirname(__file__), "recordings")
                    os.makedirs(_rec_dir, exist_ok=True)
                    ext = "wav" if "wav" in content_type else ("ogg" if "ogg" in content_type else "mp3")
                    _rec_fname = f"call_{_call_lead_id}_{int(_call_start_time)}.{ext}"
                    _rec_path = os.path.join(_rec_dir, _rec_fname)
                    with open(_rec_path, "wb") as f:
                        f.write(rec_resp.content)
                    recording_url = f"/api/recordings/{_rec_fname}"
                    ws_logger.info(f"[RECORDING] Saved direct audio: {_rec_path} ({len(rec_resp.content)} bytes)")
                    break

                # JSON response — extract RecordingUrl
                try:
                    rec_data = rec_resp.json()
                    call_obj = rec_data.get("Call", {})
                    remote_url = (
                        rec_data.get("Recording", {}).get("RecordingUrl") or
                        rec_data.get("RecordingUrl") or
                        call_obj.get("RecordingUrl")
                    )
                    if not remote_url or remote_url.strip() == "":
                        remote_url = None
                except Exception:
                    remote_url = None

                if remote_url:
                    ws_logger.info(f"[RECORDING] Got URL: {remote_url}, downloading...")
                    async with httpx.AsyncClient(timeout=30.0, follow_redirects=True) as _hc2:
                        audio_resp = await _hc2.get(remote_url, headers={"Authorization": f"Basic {auth_b64}"})
                    if audio_resp.status_code == 200 and len(audio_resp.content) > 1000:
                        _rec_dir = os.path.join(os.path.dirname(__file__), "recordings")
                        os.makedirs(_rec_dir, exist_ok=True)
                        ext = "wav" if "wav" in audio_resp.headers.get("content-type", "") else "mp3"
                        _rec_fname = f"call_{_call_lead_id}_{int(_call_start_time)}.{ext}"
                        _rec_path = os.path.join(_rec_dir, _rec_fname)
                        with open(_rec_path, "wb") as f:
                            f.write(audio_resp.content)
                        recording_url = f"/api/recordings/{_rec_fname}"
                        ws_logger.info(f"[RECORDING] Downloaded: {_rec_path} ({len(audio_resp.content)} bytes)")
                    break
                else:
                    ws_logger.info(f"[RECORDING] No URL yet, retry {_attempt+1}/6, waiting 10s...")
                    await asyncio.sleep(10)

            if not recording_url:
                ws_logger.warning(f"[RECORDING] Exotel recording not available after 6 retries, falling back to server-side recording")
            else:
                ws_logger.warning(f"[RECORDING] Exotel returned {rec_resp.status_code}")
        except Exception as _re:
            ws_logger.error(f"[RECORDING] Error fetching from Exotel: {_re}")

    # --- Server-side recording fallback — merge mic + TTS PCM into WAV ---
    if not recording_url:
        try:
            import struct, wave
            mic_chunks = _recording_mic_chunks
            tts_chunks = _tts_recording_buffers.get(stream_sid, [])
            if mic_chunks or tts_chunks:
                ws_logger.info(f"[RECORDING] Building server-side WAV: {len(mic_chunks)} mic chunks, {len(tts_chunks)} tts chunks")
                SAMPLE_RATE = 8000
                SAMPLE_WIDTH = 2  # 16-bit

                # Find time range
                all_times = [t for t, _ in mic_chunks] + [t for t, _ in tts_chunks]
                if all_times:
                    t_start = min(all_times)
                    t_end = max(all_times) + 0.5
                    total_samples = int((t_end - t_start) * SAMPLE_RATE)

                    # Create stereo buffer: left=user, right=AI
                    user_buf = bytearray(total_samples * SAMPLE_WIDTH)
                    ai_buf = bytearray(total_samples * SAMPLE_WIDTH)

                    for ts, pcm in mic_chunks:
                        offset = int((ts - t_start) * SAMPLE_RATE) * SAMPLE_WIDTH
                        end = offset + len(pcm)
                        if end <= len(user_buf):
                            user_buf[offset:end] = pcm

                    for ts, pcm in tts_chunks:
                        offset = int((ts - t_start) * SAMPLE_RATE) * SAMPLE_WIDTH
                        end = offset + len(pcm)
                        if end <= len(ai_buf):
                            ai_buf[offset:end] = pcm

                    # Interleave into stereo (L=user, R=ai)
                    stereo = bytearray(total_samples * SAMPLE_WIDTH * 2)
                    for i in range(total_samples):
                        src = i * SAMPLE_WIDTH
                        dst = i * SAMPLE_WIDTH * 2
                        stereo[dst:dst+SAMPLE_WIDTH] = user_buf[src:src+SAMPLE_WIDTH]
                        stereo[dst+SAMPLE_WIDTH:dst+SAMPLE_WIDTH*2] = ai_buf[src:src+SAMPLE_WIDTH]

                    _rec_dir = os.path.join(os.path.dirname(__file__), "recordings")
                    os.makedirs(_rec_dir, exist_ok=True)
                    _rec_fname = f"call_{_call_lead_id}_{int(_call_start_time)}.wav"
                    _rec_path = os.path.join(_rec_dir, _rec_fname)

                    with wave.open(_rec_path, 'wb') as wf:
                        wf.setnchannels(2)
                        wf.setsampwidth(SAMPLE_WIDTH)
                        wf.setframerate(SAMPLE_RATE)
                        wf.writeframes(bytes(stereo))

                    recording_url = f"/api/recordings/{_rec_fname}"
                    ws_logger.info(f"[RECORDING] Server-side WAV saved: {_rec_path} ({len(stereo)} bytes, {round(total_samples/SAMPLE_RATE, 1)}s)")
        except Exception as _wav_err:
            ws_logger.error(f"[RECORDING] Server-side WAV error: {_wav_err}")

    # --- Save transcript to DB ---
    call_duration = round(time.time() - _call_start_time, 1)
    if transcript_turns:
        transcript_id = save_call_transcript(
            lead_id=_call_lead_id,
            transcript_json=json.dumps(transcript_turns, ensure_ascii=False),
            recording_url=recording_url,
            call_duration_s=call_duration,
            campaign_id=_campaign_id,
        )

        # --- Dispatch call.completed webhook ---
        try:
            lead_info = get_lead_by_id(_call_lead_id)
            if lead_info:
                _rc2 = get_conn()
                _rcur2 = _rc2.cursor()
                _rcur2.execute("SELECT org_id FROM leads WHERE id = %s", (_call_lead_id,))
                _lead_org = _rcur2.fetchone()
                _rc2.close()
                if _lead_org and _lead_org.get('org_id'):
                    asyncio.create_task(dispatch_webhook(
                        org_id=_lead_org['org_id'],
                        event="call.completed",
                        data={
                            "lead_id": _call_lead_id,
                            "lead_name": f"{lead_info.get('first_name', '')} {lead_info.get('last_name', '')}".strip(),
                            "phone": lead_info.get('phone', ''),
                            "duration": call_duration,
                            "campaign_id": _campaign_id,
                        },
                    ))
        except Exception as _wh_err:
            ws_logger.error(f"[WEBHOOK] call.completed dispatch error: {_wh_err}")

        # --- Background call analysis with Gemini ---
        if transcript_id and _campaign_id and call_duration > 5:
            asyncio.ensure_future(_analyze_call_transcript(
                transcript_id=transcript_id,
                campaign_id=_campaign_id,
                lead_id=_call_lead_id,
                transcript_turns=transcript_turns,
                logger=ws_logger,
            ))

    # --- Auto-retry for short/failed calls ---
    _should_retry = (
        call_duration < 30  # short call = likely no-answer / busy / dropped
        or len(transcript_turns) <= 1  # no real conversation happened
    )
    if _should_retry and _call_lead_id and _campaign_id:
        try:
            retry_info = has_pending_or_exhausted_retry(_call_lead_id)
            if not retry_info['has_pending'] and not retry_info['is_exhausted']:
                # Determine attempt number (increment from last attempt if exists)
                next_attempt = retry_info.get('attempt_number', 0) + 1
                # Look up org_id for this lead
                _rc = get_conn()
                _rcur = _rc.cursor()
                _rcur.execute("SELECT org_id FROM leads WHERE id = %s", (_call_lead_id,))
                _lead_row = _rcur.fetchone()
                _rc.close()
                org_id = _lead_row['org_id'] if _lead_row else None
                if org_id:
                    call_status = "short_call" if call_duration < 30 else "no_conversation"
                    create_retry(
                        org_id=org_id,
                        lead_id=_call_lead_id,
                        campaign_id=_campaign_id,
                        last_call_status=call_status,
                        attempt_number=next_attempt,
                        max_attempts=3,
                        retry_delay_minutes=120,
                    )
                    ws_logger.info(f"[RETRY] Queued retry #{next_attempt} for lead {_call_lead_id} (duration={call_duration}s)")
            elif retry_info['has_pending']:
                ws_logger.info(f"[RETRY] Lead {_call_lead_id} already has a pending retry, skipping")
            elif retry_info['is_exhausted']:
                ws_logger.info(f"[RETRY] Lead {_call_lead_id} has exhausted max retries, skipping")
        except Exception as _retry_err:
            ws_logger.error(f"[RETRY] Error queueing retry for lead {_call_lead_id}: {_retry_err}")


async def _analyze_call_transcript(transcript_id, campaign_id, lead_id, transcript_turns, logger):
    """Send transcript to Gemini 2.5 Flash for quality analysis (runs in background)."""
    try:
        from google import genai

        transcript_text = "\n".join(
            f"{t['role']}: {t['text']}" for t in transcript_turns
        )

        analysis_prompt = f"""You are a sales call quality analyst. Analyze this AI sales call transcript and return a JSON object:

{{
  "quality_score": 1-5 (1=terrible, 5=perfect),
  "appointment_booked": true/false,
  "customer_sentiment": "positive" | "neutral" | "negative" | "annoyed",
  "failure_reason": "reason the call failed to book appointment, or null if booked",
  "what_went_well": "1-2 sentences about what the AI did right",
  "what_went_wrong": "1-2 sentences about mistakes the AI made",
  "prompt_improvement_suggestion": "specific instruction to add to the AI prompt to fix the issue"
}}

TRANSCRIPT:
{transcript_text}

Return ONLY the JSON object, no markdown, no explanation."""

        api_key = (os.getenv("GEMINI_API_KEY") or "").strip()
        if not api_key:
            logger.warning("[CALL_ANALYSIS] No GEMINI_API_KEY set, skipping analysis")
            return

        client = genai.Client(api_key=api_key)
        response = client.models.generate_content(
            model="gemini-2.5-flash",
            contents=analysis_prompt,
        )
        text = response.text.strip()
        # Strip markdown code fences if present
        if text.startswith("```"):
            text = text.split("\n", 1)[1] if "\n" in text else text[3:]
        if text.endswith("```"):
            text = text[:-3].strip()
        if text.startswith("json"):
            text = text[4:].strip()

        analysis = json.loads(text)
        save_call_review(transcript_id, campaign_id, lead_id, analysis)
        logger.info(f"[CALL_ANALYSIS] Saved review for transcript {transcript_id}: score={analysis.get('quality_score')}, sentiment={analysis.get('customer_sentiment')}")

        # --- Auto-add to DND if customer requested no more calls ---
        try:
            failure_reason = (analysis.get("failure_reason") or "").lower()
            sentiment = (analysis.get("customer_sentiment") or "").lower()
            transcript_text_lower = transcript_text.lower()
            should_dnd = False
            if any(phrase in failure_reason for phrase in ["do not call", "dnd", "don't call", "stop calling"]):
                should_dnd = True
            if sentiment in ("negative", "annoyed") and any(phrase in transcript_text_lower for phrase in [
                "don't call me", "do not call", "stop calling", "don't call again",
                "mat call karo", "call mat karo", "phone mat karo",
            ]):
                should_dnd = True
            if should_dnd:
                lead_info = get_lead_by_id(lead_id)
                if lead_info and lead_info.get("org_id") and lead_info.get("phone"):
                    add_dnd_number(lead_info["org_id"], lead_info["phone"], source="customer_request")
                    logger.info(f"[DND] Auto-added lead {lead_id} ({lead_info['phone']}) to DND — customer requested no calls")
        except Exception as dnd_err:
            logger.error(f"[DND] Auto-add error for lead {lead_id}: {dnd_err}")

        # --- WhatsApp follow-up for booked appointments ---
        if analysis.get("appointment_booked"):
            try:
                from wa_followup import send_appointment_confirmation
                await send_appointment_confirmation(lead_id=lead_id, campaign_id=campaign_id)
            except Exception as wa_err:
                logger.error(f"[WA_FOLLOWUP] Error sending appointment confirmation for lead {lead_id}: {wa_err}")

        # --- Dispatch appointment.booked webhook ---
        if analysis.get('appointment_booked'):
            try:
                lead_info = get_lead_by_id(lead_id)
                if lead_info and lead_info.get('org_id'):
                    await dispatch_webhook(
                        org_id=lead_info['org_id'],
                        event="appointment.booked",
                        data={
                            "lead_id": lead_id,
                            "lead_name": f"{lead_info.get('first_name', '')} {lead_info.get('last_name', '')}".strip(),
                            "phone": lead_info.get('phone', ''),
                            "campaign_id": campaign_id,
                        },
                    )
            except Exception as _wh_err:
                logger.error(f"[WEBHOOK] appointment.booked dispatch error: {_wh_err}")
    except Exception as e:
        logger.error(f"[CALL_ANALYSIS] Failed for transcript {transcript_id}: {e}")
