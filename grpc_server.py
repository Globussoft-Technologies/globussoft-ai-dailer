"""
grpc_server.py — Python gRPC Logic Server (Phase 0 of Go migration)

Wraps build_call_context(), generate_response_stream(), and
save_call_recording_and_transcript() behind a gRPC interface so the
Go audio service can call them via CallLogicService on port 50051.

Run alongside FastAPI:
    python grpc_server.py

FastAPI continues to serve the REST API and old WebSocket paths on :8000.
This server handles only the 4 RPCs called by the Go audio service.
"""
import asyncio
import json
import logging
import os
import re
import time
from concurrent import futures

import grpc
from dotenv import load_dotenv

# Import generated proto stubs
from callified.v1 import audio_pipeline_pb2 as pb2
from callified.v1 import audio_pipeline_pb2_grpc as pb2_grpc

# Import existing business logic
from prompt_builder import build_call_context
from llm_provider import generate_response_stream
from database import (
    get_pronunciation_context,
    get_product_knowledge_context,
    get_product_context_for_campaign,
)
from recording_service import save_call_recording_and_transcript
import rag as rag_module

load_dotenv()
logger = logging.getLogger("grpc_server")
logging.basicConfig(level=logging.INFO, format="%(asctime)s [gRPC] %(levelname)s %(message)s")

GRPC_PORT = int(os.getenv("GRPC_PORT", "50051"))

# Exotel credentials (passed through to recording service)
EXOTEL_API_KEY      = os.getenv("EXOTEL_API_KEY", "")
EXOTEL_API_TOKEN    = os.getenv("EXOTEL_API_TOKEN", "")
EXOTEL_ACCOUNT_SID  = os.getenv("EXOTEL_ACCOUNT_SID", "")

# Language → Deepgram model mapping (informational, used in InitCallResponse)
_LANG_DG_MODEL = {
    "mr": "nova-3",
}

# Sentence boundary regex — must match ws_handler.py
_SENTENCE_RE = re.compile(r'([.!?|\n]+)')

# Language → TTS language code
_TTS_LANG_CODES = {
    "hi": "hi-IN", "mr": "mr-IN", "ta": "ta-IN", "te": "te-IN",
    "bn": "bn-IN", "gu": "gu-IN", "kn": "kn-IN", "ml": "ml-IN",
    "pa": "pa-IN", "en": "en-IN",
}


def _build_context(req: pb2.InitCallRequest):
    """Replicate the context-building logic from ws_handler.py."""
    pronunciation_ctx = get_pronunciation_context()
    product_ctx = ""
    _product_persona = ""
    _product_call_flow = ""
    _call_org_id = 1
    _product_name = ""
    tts_provider = os.getenv("TTS_PROVIDER", "elevenlabs")
    tts_voice_id = os.getenv("ELEVENLABS_VOICE_ID", "")
    tts_language = _TTS_LANG_CODES.get(req.language, "hi-IN")

    try:
        from database import get_user_by_org_id
        _user_row = get_user_by_org_id(req.org_id) if req.org_id else None
        if _user_row:
            _call_org_id = _user_row.get("org_id", 1)
            tts_provider = _user_row.get("tts_provider") or tts_provider
            tts_voice_id = _user_row.get("tts_voice_id") or tts_voice_id
    except Exception:
        pass  # fallback to env defaults

    if req.campaign_id:
        try:
            _camp = get_product_context_for_campaign(req.campaign_id)
            product_ctx = _camp.get("product_ctx", "")
            _product_persona = _camp.get("agent_persona", "")
            _product_call_flow = _camp.get("call_flow_instructions", "")
            _product_name = _camp.get("product_name", "")
            camp_tts = _camp.get("tts_provider") or ""
            camp_voice = _camp.get("tts_voice_id") or ""
            if camp_tts:
                tts_provider = camp_tts
            if camp_voice:
                tts_voice_id = camp_voice
        except Exception:
            pass
        if not product_ctx:
            product_ctx = get_product_knowledge_context(org_id=_call_org_id)
    elif _call_org_id:
        product_ctx = get_product_knowledge_context(org_id=_call_org_id)
    else:
        product_ctx = get_product_knowledge_context()

    ctx = build_call_context(
        lead_name=req.lead_name,
        lead_phone=req.lead_phone,
        interest=req.interest,
        _call_lead_id=req.lead_id,
        _campaign_id=req.campaign_id or None,
        _call_org_id=_call_org_id,
        _tts_voice_override=tts_voice_id,
        product_ctx=product_ctx,
        _product_persona=_product_persona,
        _product_call_flow=_product_call_flow,
        pronunciation_ctx=pronunciation_ctx,
        _product_name=_product_name,
        _language=req.language or "hi",
    )

    return ctx, tts_provider, tts_voice_id, tts_language


class CallLogicServicer(pb2_grpc.CallLogicServiceServicer):

    def InitializeCall(self, request, context):
        logger.info("InitializeCall: lead=%s sid=%s lang=%s", request.lead_name, request.stream_sid, request.language)
        try:
            ctx, tts_provider, tts_voice_id, tts_language = _build_context(request)
        except Exception as e:
            logger.exception("InitializeCall failed: %s", e)
            context.set_code(grpc.StatusCode.INTERNAL)
            context.set_details(str(e))
            return pb2.InitCallResponse()

        return pb2.InitCallResponse(
            system_prompt=ctx.get("dynamic_context", ""),
            greeting_text=ctx.get("greeting_text", ""),
            tts_provider=tts_provider,
            tts_voice_id=tts_voice_id,
            tts_language=tts_language,
            agent_name=ctx.get("_agent_name", "अर्जुन"),
            language=request.language or "hi",
        )

    def ProcessTranscript(self, request, context):
        logger.info("ProcessTranscript: '%s...' lang=%s", request.transcript[:40], request.language)

        # Rebuild chat history from protobuf messages
        chat_history = [
            {"role": m.role, "parts": [{"text": m.text}]}
            for m in request.history
        ]

        max_tokens = request.max_tokens or (400 if request.language == "mr" else 250)

        # Run the async generator in a new event loop (gRPC uses threads)
        loop = asyncio.new_event_loop()
        asyncio.set_event_loop(loop)

        buffer = ""
        has_hangup = False

        try:
            async def _stream():
                nonlocal buffer, has_hangup
                async for chunk in generate_response_stream(
                    chat_history=chat_history,
                    system_instruction=request.system_prompt,
                    max_tokens=max_tokens,
                ):
                    buffer += chunk
                    # Split on sentence boundaries and yield complete sentences
                    parts = _SENTENCE_RE.split(buffer)
                    # parts alternates: [text, delim, text, delim, ..., remainder]
                    i = 0
                    while i + 1 < len(parts):
                        sentence = (parts[i] + parts[i + 1]).strip()
                        i += 2
                        if not sentence:
                            continue
                        hangup = "[HANGUP]" in sentence
                        if hangup:
                            has_hangup = True
                            sentence = sentence.replace("[HANGUP]", "").strip()
                        if sentence:
                            yield pb2.SentenceChunk(text=sentence, is_last=False, has_hangup=hangup)
                    buffer = parts[i] if i < len(parts) else ""

                # Flush remaining buffer
                if buffer.strip():
                    hangup = "[HANGUP]" in buffer
                    if hangup:
                        has_hangup = True
                    text = buffer.strip().replace("[HANGUP]", "").strip()
                    if text:
                        yield pb2.SentenceChunk(text=text, is_last=True, has_hangup=hangup)
                    buffer = ""
                else:
                    # Send empty final chunk so Go knows the stream is done
                    yield pb2.SentenceChunk(text="", is_last=True, has_hangup=has_hangup)

            async def _collect():
                chunks = []
                async for chunk in _stream():
                    chunks.append(chunk)
                return chunks

            chunks = loop.run_until_complete(_collect())
        finally:
            loop.close()

        yield from chunks

    def FinalizeCall(self, request, context):
        logger.info("FinalizeCall: sid=%s lead=%d duration=%.1fs", request.stream_sid, request.lead_id, request.call_duration_s)

        chat_history = [
            {"role": m.role, "parts": [{"text": m.text}]}
            for m in request.chat_history
        ]

        # Server-side stereo WAV: write to disk if provided by Go
        mic_chunks = []
        tts_buffers = {request.stream_sid: []}
        if request.has_server_recording and request.stereo_wav:
            fname = f"recordings/call_{request.lead_id}_{int(time.time())}.wav"
            try:
                with open(fname, "wb") as f:
                    f.write(request.stereo_wav)
                logger.info("Saved server-side WAV: %s", fname)
            except Exception as e:
                logger.warning("Failed to save WAV: %s", e)

        loop = asyncio.new_event_loop()
        asyncio.set_event_loop(loop)
        try:
            recording_url, transcript_id = loop.run_until_complete(
                save_call_recording_and_transcript(
                    stream_sid=request.stream_sid,
                    _call_lead_id=request.lead_id,
                    _exotel_call_sid=request.call_sid,
                    chat_history=chat_history,
                    _recording_mic_chunks=mic_chunks,
                    _tts_recording_buffers=tts_buffers,
                    _call_start_time=time.time() - request.call_duration_s,
                    EXOTEL_API_KEY=EXOTEL_API_KEY,
                    EXOTEL_API_TOKEN=EXOTEL_API_TOKEN,
                    EXOTEL_ACCOUNT_SID=EXOTEL_ACCOUNT_SID,
                    _campaign_id=request.campaign_id or None,
                )
            )
        except Exception as e:
            logger.exception("FinalizeCall save failed: %s", e)
            recording_url = ""
            transcript_id = 0
        finally:
            loop.close()

        return pb2.FinalizeCallResponse(
            recording_url=recording_url or "",
            transcript_id=transcript_id or 0,
        )

    def RetrieveContext(self, request, context):
        try:
            ctx = rag_module.retrieve_context(request.query, request.org_id, top_k=2)
        except Exception as e:
            logger.warning("RAG failed: %s", e)
            ctx = ""
        return pb2.RAGResponse(context=ctx or "")


def serve():
    server = grpc.server(futures.ThreadPoolExecutor(max_workers=10))
    pb2_grpc.add_CallLogicServiceServicer_to_server(CallLogicServicer(), server)
    server.add_insecure_port(f"[::]:{GRPC_PORT}")
    server.start()
    logger.info("gRPC CallLogicService listening on :%d", GRPC_PORT)
    try:
        server.wait_for_termination()
    except KeyboardInterrupt:
        logger.info("gRPC server shutting down")
        server.stop(grace=5)


if __name__ == "__main__":
    serve()
