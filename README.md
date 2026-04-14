# 🚀 Globussoft Generative AI Dialer

A full-stack, AI-native CRM designed to fully automate telecom sales, field ops geofencing, and internal workflows under the Globussoft architecture.

## 🌍 Live Environments
- **Production Development:** `https://test.callified.ai`
- **Frozen Sales Demo:** `https://demo.callified.ai` (Running Release `v1.0.0-demo`)


## 📞 Live AI Call Flow Schematic
```mermaid
sequenceDiagram
    participant CRM as Universal CRM (100+ Providers)
    participant Dialer as FastApi Backend Engine
    participant Human as Target Lead (Human)
    participant STT as Deepgram (Speech-to-Text)
    participant LLM as Gemini 2.5 (AI Brain)
    participant TTS as ElevenLabs (Voice)

    CRM->>Dialer: 1. Background Poller retrieves untested leads
    loop The AI Call Cycle
        Dialer->>Human: 2. Initiates Telecom Call (Twilio/Exotel)
        Human-->>Dialer: 3. Lead picks up the phone
        
        rect rgba(0, 0, 0, 0.05)
        loop Active Real-Time Conversation Stream
            Human->>Dialer: Speaks (e.g. "I'm looking for a 3BHK")
            Dialer->>STT: Streams raw audio via WebSockets
            STT-->>Dialer: Transcribes speech instantly
            Dialer->>LLM: Ingests transcript & custom Sales Prompt
            LLM-->>Dialer: Generates exact response & qualification state
            Dialer->>TTS: Converts AI text to audio stream
            TTS-->>Dialer: Streams realistic voice bytes
            Dialer->>Human: Plays audio to human speaker seamlessly
        end
        end
    end

    Human->>Dialer: 4. Human hangs up
    Dialer->>LLM: 5. Submits full transcript for final analysis
    LLM-->>Dialer: 6. Returns qualification status & smart follow-up notes
    Dialer->>CRM: 7. Mutates CRM Object (Status to Warm/Closed + Note)
```

## 🧠 Core System Flow & Microservices

```mermaid
graph TD;
    User/Browser-->|HTTP/REST| API(fastAPI: routes.py)
    API-->DB(database.py: MySQL)
    User/Browser-->|WebSocket| WS(ws_handler.py)
    Exotel-->|Raw WebSocket| WS(ws_handler.py)
    
    WS-->Deepgram(STT: DeepgramClient)
    Deepgram-->|Transcription event| LLM(LLM_Provider: Gemini/Groq)
    LLM-->TTS(TTS Engine: ElevenLabs)
    TTS-->|PCM Audio stream| WS
    WS-->|Playback bytes| Exotel
    
    API-->RAG(rag.py: FAISS/Transformers)
    RAG-->DB
```

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


## Features Developed

1. **Multilingual AI Voice Agent (Dialer)**
   - Unified Outbound caller supporting both **Twilio** and **Exotel**.
   - Bidirectional real-time `media-stream` webSockets.
   - Powered by Gemini 2.5 LLM context and Deepgram transcription.

2. **Automated Exotel Call Summarizer**
   - Automatically catches completed `.mp3` recordings from Exotel.
   - Transcribes Indian English, Hindi, and Bengali using Deepgram `nova-3`.
   - Summarizes the transcript into distinct *Client Sentiment*, *Budget*, and *Next Steps* using Gemini.
   - Injects the AI Follow-Up Note permanently into the CRM SQLite Database.

3. **Geofenced Field Operations Module**
   - HTML5 `navigator.geolocation` integration for agent site-visits.
   - FastAPI `haversine` formula verifies whether the agent's GPS coordinates are precisely within 500m of a designated Site.
   - Accurate, un-spoofable attendance logging directly attached to the CRM.

4. **Cross-Department Workflow Engine**
   - Automatically monitors CRM Lead stages.
   - Auto-generates Internal Kanban Tickets for `Legal`, `Accounts`, and `Housing Loan` teams when Deals are Closed.
   - Real-time React KPI Reporting.

5. **WhatsApp Automation Triggers (Mocked)**
   - Smart backend engine that fires structural WhatsApp Nudges.
   - For example: Automatically texts Property e-Brochures when an AI categorizes a Lead as "Warm".
   - Viewable via a WhatsApp-Web styled UI within the Dashboard.

6. **CRM Document Vault**
   - Natively attach files and compliance agreements to specific Leads.
   - Distinct SQLite mappings for secure retrieval (`Aadhar`, `PAN`, `Sales Agreements`).
   - Unified Modal UI injected straight into the core CRM.

7. **Visual Data Analytics Center**
   - Natively rendered, dynamic CSS Flexbox charting engine.
   - Visualizes "Call Volume vs. Closed Deals" 7-day trailing trends.
   - Zero-dependency executive monitoring portal for internal stakeholders.

8. **Global Smart Search Query API**
   - Universal parameter-based SQLite matching engine (`LIKE %...%`).
   - Find Clients by exact Name, substring, or direct Phone Number matches instantly.
   - Beautiful dashboard search-bar state mutation architecture.

9. **Database CSV Export Engine**
   - High-speed Python pipeline converting SQLite arrays into downloadable Dataframes.
   - Streams native `.csv` files via FastApi directly to the Sales Director's local machine.

10. **Role-Based Access Control (RBAC)**
    - Enterprise security UI guardrails hiding sensitive PII and executive data.
    - Simulated `[Admin]` vs `[Agent]` viewer contexts to lock down database export and global metrics routes natively.

11. **Manual Quick Notes System**
    - Instantaneous human-override timeline logging.
    - Allows agents to bypass the LLM Voice agent and directly manually update Client profiles post-call.

12. **GenAI One-Click Email Drafter**
    - Autonomously drafts hyper-personalized follow-up emails based on SQLite timeline history.
    - Leverages Gemini 1.5 Flash natively directly inside the React table.

13. **Sub-Second Latency Audio Streaming**
    - Integrates background Python `asyncio.Queue` chunkers to immediately synthesize and pipeline Elevenlabs audio by NLTK-parsed sentences *while* the LLM is actively streaming tokens. Achieving real-time phone calls with Time-to-First-Byte audio latency under 800ms.

14. **Clean Componentized React Architecture**
    - The monolith Dashboard is split cleanly into discrete `<CrmTab />`, `<OpsTab />`, and `<SettingsTab />` JSX modules.

15. **Local FAISS Knowledge Base (RAG)**
    - Performs localized vector retrieval without external databases.
    - Uses `sentence-transformers` to chunk and embed `.pdf` documents directly into the local `faiss_indexes` folder for lightning-fast knowledge ingestion.

16. **Deep Backend WebSocket Automation Tests**
    - Simulates Exotel bytes and Sandbox WebSockets directly inside the ASGI thread.
    - Intercepts LLM, STT, and TTS engines down to the microsecond level so development CI/CD pipelines run natively without live API costs.

17. **Server-Side Stereo Call Recording**
    - Captures raw PCM audio from both sides of the conversation in real-time during live AI calls.
    - User audio (phone mic via Exotel/Twilio WebSocket) is captured on the **left channel**, AI audio (TTS synthesis output) on the **right channel**.
    - Merges both streams into a timestamped stereo `.wav` file at 8000Hz 16-bit — standard telephony quality with zero re-encoding loss.
    - Acts as an automatic fallback when the telecom provider's recording API is unavailable or delayed, ensuring 100% call recording coverage.
    - Stereo separation enables independent analysis of agent vs. customer speech for QA, compliance, and training.

18. **Multi-Provider TTS Engine (ElevenLabs, Sarvam AI, SmallestAI)**
    - Hot-swappable TTS providers selectable per-organization from the dashboard.
    - **Sarvam AI Bulbul v3**: Native Hindi/Indian language TTS with 25+ voices via WebSocket streaming. Best for natural Devanagari pronunciation.
    - **ElevenLabs Turbo v2.5**: Premium multilingual voice quality with natural question intonation. Best for Hinglish prosody.
    - **SmallestAI Lightning**: Sub-100ms latency Indian voices. Best for ultra-low latency telephony.
    - Gender-aware AI persona automatically switches name, pronouns, and grammar based on selected voice (male: Arjun, female: Priya).

19. **Redis-Backed Call State for Horizontal Scaling**
    - Serializable call state (pending call metadata, takeover flags, whisper queues) stored in Redis with TTL-based auto-cleanup.
    - Non-serializable state (asyncio Tasks, WebSocket connections) stays in-memory per process.
    - Graceful fallback to in-memory dicts if Redis is unavailable — zero downtime on Redis failure.

20. **Comprehensive Playwright E2E Test Suite (19 Tests)**
    - Full browser automation testing against the live production environment.
    - Covers: Auth (signup/logout), CRM (add/edit/delete/search leads), Settings (products, pronunciation), Ops, Analytics, WhatsApp, Integrations tabs, and CRM modals (transcripts, documents, notes).
    - Auto-cleanup fixture removes test data after each session.
    - GitHub Actions CI pipeline runs all tests on every push.

## 🐳 Docker Local Deployment (Recommended)

The fastest way to run the full stack locally — no manual MySQL/Redis setup required. Requires **Docker Desktop** and **Docker Compose v2**.

### 1. Copy the environment template

```bash
cp .env.example .env.docker
```

Edit `.env.docker` and fill in your real API keys (Gemini/Groq, Deepgram, ElevenLabs, Twilio/Exotel) and passwords. The internal service hostnames (`MYSQL_HOST=db`, `REDIS_URL=redis://:...@redis:6379/1`) are pre-wired for docker-compose and should not be changed.

> `.env.example` contains all required variables with placeholder values. It is safe to commit. Never commit `.env.docker` — it is gitignored.

### 2. Build and start all services

```bash
docker compose --env-file .env.docker up --build
```

This will:
- Build the **React frontend** (Node 20) and embed it into the backend image
- Start **MySQL 8.0** on port `3307` (avoids conflicts with any local MySQL on 3306)
- Start **Redis 7** on port `6380`
- Start the **FastAPI backend** on port `8001` (waits for DB + Redis health checks)
- Pre-download the `sentence-transformers/all-MiniLM-L6-v2` model at build time

First build takes ~5–10 minutes (downloading base images + pip install). Subsequent builds are cached.

### 3. Verify the services are healthy

```bash
# Overall health check
curl http://localhost:8001/ping

# Detailed service health (DB, Redis, scheduler, retry worker)
curl http://localhost:8001/api/debug/health
```

### 4. Seed the first admin user

On a fresh database, create your first organization and admin account:

```bash
docker compose exec app python - <<'EOF'
from database import get_conn
from auth import get_password_hash
import datetime

conn = get_conn()
cur = conn.cursor()

# Create organization
cur.execute("INSERT INTO organizations (name, timezone) VALUES (%s, %s)", ("My Org", "Asia/Kolkata"))
org_id = cur.lastrowid

# Create admin user
cur.execute(
    "INSERT INTO users (org_id, full_name, email, password_hash, role) VALUES (%s, %s, %s, %s, %s)",
    (org_id, "Admin", "admin@example.com", get_password_hash("yourpassword"), "admin")
)
conn.commit()
cur.close()
conn.close()
print(f"Created org_id={org_id}, login: admin@example.com / yourpassword")
EOF
```

### 5. Access the app

Open `http://localhost:8001` in your browser and log in with the credentials you set in step 4.

### Useful Docker commands

```bash
# Run in detached (background) mode
docker compose --env-file .env.docker up -d

# View live logs
docker compose logs -f app

# Stop all services
docker compose down

# Stop and wipe all data volumes (full reset)
docker compose down -v

# Rebuild after code changes
docker compose --env-file .env.docker up --build app
```

### Service port map

| Service | Internal port | Exposed locally |
|---------|--------------|-----------------|
| FastAPI app | 8001 | 8001 |
| MySQL 8.0 | 3306 | **3307** |
| Redis 7 | 6379 | **6380** |

Connect to MySQL locally: `mysql -h 127.0.0.1 -P 3307 -u callified -p<YOUR_MYSQL_PASSWORD> callified_ai`

---

## 🔁 Docker Dev Mode (Hot Reload)

A `docker-compose.override.yml` file is included that enables full hot-reload for **both** backend and frontend inside Docker. It is picked up **automatically** — no extra flags needed.

**What it does:**
- **Backend** — mounts repo into the `app` container, runs uvicorn with `--reload` so any `.py` save triggers an instant restart
- **Frontend** — adds a dedicated `frontend` service (`node:20-alpine`) running the Vite dev server with full HMR; any `frontend/src/` save updates the browser instantly
- Drops `--loop uvloop` from uvicorn (incompatible with `--reload`)
- Disables `restart: always` on both services so crashes stay visible in logs

> **Note:** `watchfiles` is required for uvicorn `--reload`. It is in `requirements.txt` (`uvicorn[standard]` + `watchfiles`). Run `--build` once after a fresh pull.

### Starting the dev stack

```bash
docker compose --env-file .env.docker up app frontend
```

This starts **4 containers** total: `db`, `redis`, `app` (backend), `frontend` (Vite HMR).

**Open `http://localhost:5173` in your browser** — not 8001. The Vite dev server proxies all `/api`, `/ws`, `/ping` and `/recordings` requests to the FastAPI backend on port 8001.

### Port map in dev mode

| Container | What it serves | URL |
|-----------|---------------|-----|
| `frontend` | React app (Vite HMR) | **http://localhost:5173** ← use this |
| `app` | FastAPI + last built static files | http://localhost:8001 |
| `db` | MySQL 8.0 | localhost:3307 |
| `redis` | Redis 7 | localhost:6380 |

### Backend hot-reload

Edit any `.py` file and save. You'll see in the logs within ~1 second:
```
WARNING:  WatchFiles detected changes in 'routes.py', reloading...
INFO:     Application startup complete.
```

### Frontend HMR

Edit any file under `frontend/src/` and save. The browser updates instantly without a full page reload — React state is preserved where possible.

### First-time build (or after adding pip packages)

```bash
docker compose --env-file .env.docker up --build app frontend
```

### Skip the override (production build)

```bash
docker compose --env-file .env.docker -f docker-compose.yml up --build
```

### Summary

| Change type | Action needed | Reload type |
|-------------|--------------|-------------|
| Backend `.py` file | Save the file | uvicorn restarts (~1s) |
| `frontend/src/` file | Save the file | Vite HMR (instant, no page reload) |
| New pip package in `requirements.txt` | `docker compose up --build app` | Full image rebuild |
| New npm package in `package.json` | `docker compose restart frontend` | npm install + Vite restart |
| Env var in `.env.docker` | `docker compose up app frontend` | No rebuild needed |

---

## 🐛 Docker Troubleshooting

### `Unknown column 'tts_provider' in 'field list'`

**Cause:** The `organizations` table was created before voice-settings columns were added. `CREATE TABLE IF NOT EXISTS` won't add new columns to an existing table.

**Fix (automatic):** `init_db()` in `database.py` runs `ALTER TABLE organizations ADD COLUMN` migrations on every startup with try/except — the columns are added automatically on the next container start. If you still see the error, restart the app container:

```bash
docker compose restart app
```

### `ImportError: cannot import name 'LiveTranscriptionEvents' from 'deepgram'`

**Cause:** `deepgram-sdk` v6 (Fern-generated rewrite) removed `LiveTranscriptionEvents`. This app requires v3.x.

**Fix:** `requirements.txt` pins `deepgram-sdk>=3.0.0,<4.0.0`. Rebuild the image:

```bash
docker compose --env-file .env.docker up --build app
```

### `--reload` not watching files / uvicorn starts but doesn't restart on save

**Cause 1:** `watchfiles` not installed in the image (built before it was added to `requirements.txt`).
**Fix:** `docker compose --env-file .env.docker up --build app`

**Cause 2:** `--loop uvloop` and `--reload` were both set — they are mutually exclusive.
**Fix:** Already resolved in `docker-compose.override.yml` — `--loop uvloop` is omitted in dev mode.

### App container exits immediately with low memory (~25 MB shown in `docker ps`)

**Cause:** uvicorn spawned with `--workers N` causes subprocess crashes that PM2/Docker shows as "running" while the actual worker is dead.

**Fix:** Do not use `--workers` in Docker. The `CMD` in `Dockerfile` and the override both use a single process.

---

## 🛠 Manual Local Setup (Without Docker)

Follow these instructions to set up, run, and test the Generative AI Dialer locally without Docker.

### Prerequisites

You will need the following installed on your machine:
- **Node.js** (v16 or higher)
- **Python 3.10** (required — 3.9 may work but is untested)
- **MySQL 8.0**
- **Redis 7**
- **Git**

You will also need accounts and API keys for the following external services:
- **Twilio** or **Exotel** (For telecom/dialing)
- **Deepgram** (For prompt Speech-to-Text)
- **Google AI Studio / Gemini** (For the core conversation and sales LLM logic)
- **ElevenLabs** (For realistic Voice/TTS)
- **Ngrok** (For localhost tunneling to receive call webhooks)

### 1. Clone the Repository
```bash
git clone <your-repository-url>
cd gbs-ai-dialer
```

### 2. Environment Configuration
Create a `.env` file in the root of the project (the same folder as `main.py`) and populate it exactly as follows with your own credentials:

```ini
# --- TELECOM PROVIDERS ---
# Which dialer to use by default: 'twilio' or 'exotel' (Currently Exotel on Live)
DEFAULT_PROVIDER=exotel

# Twilio Configuration
TWILIO_ACCOUNT_SID=your_twilio_sid
TWILIO_AUTH_TOKEN=your_twilio_auth_token
TWILIO_PHONE_NUMBER=your_twilio_phone_number

# Exotel Configuration (If using Exotel instead of Twilio)
EXOTEL_API_KEY=your_exotel_api_key
EXOTEL_API_TOKEN=your_exotel_api_token
EXOTEL_ACCOUNT_SID=your_exotel_account_sid
EXOTEL_CALLER_ID=your_exotel_caller_id

# --- AI SERVICES PIPELINE ---
# Model Routing
LLM_PROVIDER=groq
TTS_PROVIDER=elevenlabs

# API Keys
DEEPGRAM_API_KEY=your_deepgram_api_key
GROQ_API_KEY=your_groq_api_key
GEMINI_API_KEY=your_gemini_api_key
ELEVENLABS_API_KEY=your_elevenlabs_api_key
ELEVENLABS_VOICE_ID=your_elevenlabs_voice_id
SMALLEST_API_KEY=your_smallest_ai_key
SMALLEST_VOICE_ID=your_smallest_ai_voice_id

# --- NETWORKING & AUTH ---
# Server domain for Webhook Audio Streams (Ngrok or test.callified.ai)
PUBLIC_SERVER_URL=https://your-ngrok-url.ngrok-free.app
PUBLIC_URL=https://your-ngrok-url.ngrok-free.app
JWT_SECRET=your_secure_jwt_secret
MYSQL_HOST=localhost
MYSQL_USER=callified
MYSQL_PASSWORD=your_password
MYSQL_DATABASE=callified_ai
```

### 3. Start Ngrok (Webhook Tunneling)
For Twilio or Exotel to reach your local backend during a live call, you must expose your local port `8000` to the public internet securely.
```bash
ngrok http 8000
```
*Copy the `Forwarding` HTTPS URL (e.g., `https://abc-123.ngrok.app`) and paste it into the `PUBLIC_SERVER_URL` variable in your `.env` file.*

### 4. Run the Backend (FastAPI)
Open a terminal in the root directory:
```bash
# Create and activate a virtual environment
python -m venv .venv

# On Windows:
.venv\Scripts\activate
# On Mac/Linux:
# source .venv/bin/activate

# Install dependencies
pip install -r requirements.txt

# Run the server
uvicorn main:app --reload --port 8000
```

### 5. Run the Frontend (React)
Open a new terminal session:
```bash
cd frontend
npm install
npm run dev -- --port 5173
```
Visit `http://localhost:5173` in your browser to view the AI Dialer CRM Dashboard!

### 6. Linking Webhooks (Crucial Step)
Depending on your chosen provider, you must confirm their webhook configurations so the AI can intercept the call.

**If using Twilio:**
You do *not* need to manually configure the webhook endpoint on the Twilio dashboard. The application dynamically builds and passes the webhook (`/webhook/twilio`) during the API call initiation! Just ensure your `PUBLIC_SERVER_URL` inside the `.env` is perfectly accurate and your `TWILIO_PHONE_NUMBER` is verified on Twilio if using a trial account.

**If using Exotel:**
You must configure an Exotel **VoiceBot Applet** in your Exotel call flow visualizer. The applet should point its WebSocket URL to:
`wss://<YOUR-NGROK-URL-WITHOUT-HTTPS>/media-stream`
When the call connects, Exotel will stream the audio down this WebSocket connecting the client perfectly to the AI.

### 7. Automated E2E Testing
The project abandons traditional local mocking in favor of two controllable End-to-End Test suites that execute real database permutations and API testing natively across environments.
To verify codebase integrity, run one of the two top-level triggers based on the environment you are validating:

```bash
# Validate core backend WebSockets (Sandbox + Exotel Streams natively via TestClient)
# Uses mock providers to verify STT/TTS routing without burning API tokens.
python -m pytest tests/e2e/test_ws_core.py -v --cov=ws_handler --cov-report=term-missing

# E2E Script triggers for full environment runs:
# Validates your active localhost (http://localhost:8000)
python run_local_e2e.py

# Validates the active remote sandbox (https://test.callified.ai)
python run_server_e2e.py
```
