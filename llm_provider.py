"""
Pluggable LLM provider for the AI Dialer.

Supports:
- gemini: Google Gemini 2.5 Flash (default)
- groq: Groq LPU running Llama 3.3 70B (~50ms inference)

Switch via env var: LLM_PROVIDER=groq  (default: gemini)
"""

import os
import logging

logger = logging.getLogger("uvicorn.error")

LLM_PROVIDER = os.getenv("LLM_PROVIDER", "gemini").lower()

# ─── Groq Provider ───────────────────────────────────────────────────────────

async def _groq_generate(chat_history: list, system_instruction: str, max_tokens: int = 150) -> str:
    """Generate response using Groq (Llama 3.3 70B)."""
    from groq import AsyncGroq

    client = AsyncGroq(api_key=os.getenv("GROQ_API_KEY"))

    # Convert Gemini chat format to OpenAI format (which Groq uses)
    messages = [{"role": "system", "content": system_instruction}]
    for entry in chat_history:
        role = entry.get("role", "user")
        text = ""
        parts = entry.get("parts", [])
        if parts and isinstance(parts[0], dict):
            text = parts[0].get("text", "")
        elif isinstance(parts, str):
            text = parts

        if role == "model":
            messages.append({"role": "assistant", "content": text})
        else:
            messages.append({"role": "user", "content": text})

    model = os.getenv("GROQ_MODEL", "llama-3.3-70b-versatile")
    response = await client.chat.completions.create(
        model=model,
        messages=messages,
        max_tokens=max_tokens,
        temperature=0.7,
    )

    return response.choices[0].message.content


# ─── Gemini Provider ─────────────────────────────────────────────────────────

async def _gemini_generate(chat_history: list, system_instruction: str, max_tokens: int = 150) -> str:
    """Generate response using Gemini 2.5 Flash."""
    from google import genai
    from google.genai import types

    client = genai.Client(api_key=os.getenv("GEMINI_API_KEY"))
    model = os.getenv("GEMINI_MODEL", "gemini-2.5-flash")

    response = await client.aio.models.generate_content(
        model=model,
        contents=chat_history,
        config=types.GenerateContentConfig(
            system_instruction=system_instruction,
            max_output_tokens=max_tokens,
        ),
    )

    return response.text


# ─── Public API ──────────────────────────────────────────────────────────────

async def generate_response(chat_history: list, system_instruction: str, max_tokens: int = 150) -> str:
    """
    Generate LLM response using the configured provider.
    Falls back to Gemini if Groq hits rate limits.

    Returns the response text string.
    Raises on error (caller should handle).
    """
    provider = LLM_PROVIDER
    logger.info(f"[LLM] Using provider: {provider}")

    if provider == "groq":
        try:
            return await _groq_generate(chat_history, system_instruction, max_tokens)
        except Exception as e:
            if "429" in str(e) or "rate_limit" in str(e).lower():
                logger.warning(f"[LLM] Groq rate limited, falling back to Gemini: {str(e)[:80]}")
                return await _gemini_generate(chat_history, system_instruction, max_tokens)
            raise
    else:
        return await _gemini_generate(chat_history, system_instruction, max_tokens)
