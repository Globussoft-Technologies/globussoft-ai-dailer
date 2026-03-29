import sys

with open("README.md", "r", encoding="utf-8") as f:
    content = f.read()

subsystems_text = """

### 1. `main.py`
Acts as the central orchestrator and ASGI app.
* **Bootstrapping**: Initializes the FastAPI app, manages environment variables (`EXOTEL_API_KEY`, etc.), and mounts sub-routers (`auth.py`, `routes.py`, `live_logs.py`, `ws_handler.py`).
* **Background Process**: Defines `poll_crm_leads()` which runs as an `asyncio.create_task` loop inside the main process to check external CRM APIs every 60 seconds for new leads.
* **Dial Management**: Includes fallback methods for WhatsApp triggering and bridging out to Twilio/Exotel via REST before the call shifts to WebSockets.

### 2. `ws_handler.py` (The Heart of Realtime)
Handles the full-duplex bi-directional streaming of AI calls.
* **Connections**: Listens on `/ws/sandbox` (React microphone testing) and `/media-stream` (Exotel raw μ-law testing).
* **Pipeline Integration**: Re-packages raw byte packets and ships them to Deepgram for live transcription. When Deepgram issues an `on_message` callback, the handler hits `llm_provider.py` and streams those chunks dynamically into `tts.py`.
* **State Management**: Uses memory dictionaries like `whisper_queues`, `active_tts_tasks`, and `takeover_active` to manage asynchronous racing conditions between AI replies and human barge-in ("listening...").

### 3. `database.py`
The sole persistence layer of the app.
* Runs on pure `pymysql` with raw SQL queries mapping to `callified_ai`.
* Handles over 15 distinct entities: `leads`, `calls`, `tasks`, `documents`, `products`, `knowledge_base`, `pronunciation_guide`, etc.
* **Domain Triggers**: Embeds domain-logic inside writes (e.g. cross-department automation when `status="Closed"` or WhatsApp Nudge generation when `status="Warm"`).

### 4. `routes.py`
Exposes the CRUD endpoints for your Next.js Frontend.
* Contains `/api/leads`, `/api/tasks`, `/api/products`, `/api/knowledge/upload`, etc.
* **Scraping Capability**: Implements an HTTP scraping crawler inside `/api/products/{product_id}/scrape` using Llama-3 parsing when product pages are linked.
* Includes a fully replicated Mobile API namespace via `APIRouter(prefix="/api/mobile")`.

### 5. `rag.py` & Vector Search
The local Knowledge Base Retrieval tool.
* Bypasses heavy cloud vector databases by utilizing local `faiss` indices.
* Embeds documents using the lightweight, open-source `sentence-transformers` (`all-MiniLM-L6-v2`) locally within the CPU environment.
* Generates `.index` dumps and metadata inside a dynamically created `/faiss_indexes/` repository folder.

### 6. `tts.py` & `llm_provider.py`
External Model Clients.
* **`tts.py`**: Fetches Voice Settings from the database context and fires off streaming requests to ElevenLabs or Google Cloud TTS, ensuring the audio is returned in the precise sample rate chunked formats (`PCM 16000` or `PCM 8000 mu-law`).
* **`llm_provider.py`**: A fallback wrapper that defaults to Groq (Llama-3 70b) and falls back to Gemini `1.5-flash` natively to ensure 99% uptime on generation.
"""

target = "    RAG-->DB\n```"
content = content.replace(target, target + subsystems_text)
content = content.replace(target.replace("\n", "\r\n"), (target + subsystems_text).replace("\n", "\r\n"))

with open("README.md", "w", encoding="utf-8") as f:
    f.write(content)

print("Updated README.md")
