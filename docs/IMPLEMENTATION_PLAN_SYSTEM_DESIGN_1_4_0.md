# Callified System Design & Implementation Plan v1.4.0

## Executive Summary

This document proposes a set of architectural and implementation changes for the Callified AI Dialer platform to resolve the most pressing live issues, improve reliability, and prepare the product for scale. The plan covers the **frontend**, **backend**, and **AI pipeline**, with concrete file references and a phased rollout.

## Implementation Status

- **Phase 1 (partially complete)** — merged in PR #97 to `dev_1.3.0`.
  - Done: provider abstraction, credential validation on save, clearer dial errors, `RecordingStorage` interface, `MYSQL_PASSWORD_FILE` support.
  - Pending: full account-priority resolution, pre-signed recording URLs, storage health check on startup, WebSocket authentication, MySQL password rotation.
- **Phase 2 (merged)** — PRs #98 and #99 merged to `dev_1.3.0`.
  - Done: `CallState` enum and `CallManager` package, call-state transitions, greeting-guard prompt patch, Redis-backed dial queue, retry logic, pause/resume/abort, progress panel, TRAI check removed.
  - Pending: locked language per call, full `ConversationState` struct.
- **Phase 3 (done)** — TanStack Query migration, central route config, lazy loading, bundle below 500 KB.
  - Done: `@tanstack/react-query` dependency, query hooks (`useCampaigns`, `useCampaign`, `useLeads`, `useCallLogs`, `useAgentReport`, `useOrganizations`, `useOrgProducts`), React Query provider wiring, `OrgContext` refactored, `EventContext` invalidates affected query keys, route table + `ProtectedRoute`, `App.jsx` refactored with lazy-loaded pages, `CampaignsPage`/`CampaignDetail`/`AgentReportPage` use query hooks, Vite chunk splitting, initial bundle 357 kB (gzipped 85 kB).
  - Pending: extend React Query to remaining ad-hoc fetches (team, executives, products, billing, etc.), agent-presence events.

## Current Pain Points Observed

Based on recent testgo1, testgo2, and app.callified.ai issues:

1. **AI Dial failures**: Exotel 401/502, "no campaign Exotel credentials set".
2. **Manual/AI account confusion**: Route guards and menu items scattered; provider accounts duplicated in UI.
3. **Repeated greetings and language switching**: AI greets twice or changes language mid-call.
4. **Auto-dial interruption**: Manual Save/Next popups break batch calling.
5. **Stale dashboards**: Lead status updates do not reflect in real time on dashboards/reports.
6. **Agent-level access gaps**: Leads assigned to agents are still visible/callable by admins.
7. **Duplicate leads in campaigns**: Campaign lists show duplicate phone numbers.
8. **Recording link issues**: Oracle bucket URLs fail or are not generated correctly.
9. **Bulk upload failures**: CSV imports fail due to missing columns or format mismatches.
10. **Security debt**: Hardcoded DB password in `backend/.env`, unauthenticated WebSockets, unsigned webhooks.

---

## 1. Frontend Architecture

### 1.1 Replace Context-based server state with React Query

**Current state**
- `AuthContext.jsx`, `CallContext.jsx`, `OrgContext` manage server state via `useEffect` + `fetch`.
- Components poll independently, causing race conditions and stale UI.

**Target state**
- Introduce **TanStack Query (React Query)** for all server state.
- Define query keys per entity: `['org']`, `['campaigns']`, `['campaign', id]`, `['campaign', id, 'leads']`, `['callLogs']`, `['agentReport']`.

**Benefits**
- Automatic background refetch, cache invalidation, deduped requests.
- Single source of truth for lead/campaign/call data.
- Removes ~60% of ad-hoc `useEffect` fetches.

**Files to change**
- `frontend/src/contexts/AuthContext.jsx`
- `frontend/src/contexts/CallContext.jsx`
- `frontend/src/components/campaigns/CampaignDetail.jsx`
- `frontend/src/components/tabs/CrmTab.jsx`
- `frontend/src/pages/AgentReportPage.jsx`
- `frontend/src/pages/CampaignsPage.jsx`

**Migration steps**
1. Add `@tanstack/react-query` to `frontend/package.json`.
2. Wrap `App` in `QueryClientProvider`.
3. Create query hooks:
   - `useCampaigns()`
   - `useCampaign(id)`
   - `useLeads(campaignId, filters)`
   - `useCallLogs(filters)`
   - `useAgentReport(filters)`
4. Replace direct `apiFetch` in components with these hooks.
5. Mutations invalidate related query keys.

### 1.2 Real-time updates via Server-Sent Events (SSE)

**Current state**
- Dashboards and campaign pages require manual refresh.

**Target state**
- Single SSE connection `/api/events` per browser session.
- Backend pushes domain events; frontend invalidates affected React Query keys.

**Event types**
```json
{ "type": "LEAD_STATUS_CHANGED", "campaignId": 46, "leadId": 123, "status": "Qualified", "executiveId": 5 }
{ "type": "CALL_COMPLETED", "campaignId": 46, "leadId": 123, "outcome": "Interested", "duration": 42 }
{ "type": "AGENT_PRESENCE_CHANGED", "agentId": 5, "status": "on_call" }
```

**Files to change**
- Frontend: `frontend/src/contexts/EventContext.jsx` (new)
- Backend: `backend/internal/api/events.go` (new), `backend/internal/redis/store.go`

### 1.3 Centralize route permissions

**Current state**
- `App.jsx` has `hideAiFeatures ? <Navigate to="/crm" replace />` repeated per route.

**Target state**
- Define route table with `permission`, `requiresAiFeatures`, `allowedRoles`.
- Single `<ProtectedRoute />` component handles gating.

**Example route table**
```js
const routes = [
  { path: '/crm', element: CrmPage, roles: ['Admin','TeamLeader','Agent','Executive'] },
  { path: '/products', element: ProductsPage, roles: ['Admin'] },
  { path: '/analytics', element: AnalyticsPage, roles: ['Admin','TeamLeader'], permission: 'reports.view' },
  { path: '/exotel-accounts', element: ExotelAccountsPage, roles: ['Admin'], permission: 'provider_accounts.global', aiFeatures: false },
  { path: '/monitor', element: MonitorPage, aiFeatures: true },
];
```

**Files to change**
- `frontend/src/App.jsx`
- `frontend/src/utils/routeConfig.js` (new)
- `frontend/src/components/ProtectedRoute.jsx` (new)

### 1.4 Bundle splitting

**Current state**
- Single 862 KB JS chunk.

**Target state**
- Lazy-load heavy pages: Analytics, Agent Report, Team, User Management, Receptionist.

**Files to change**
- `frontend/src/App.jsx`
- `frontend/src/pages/*.jsx` imports

### 1.5 Persist call-action preferences server-side

**Current state**
- `SettingsTab.jsx` stores `callified_call_actions` in `localStorage`.

**Target state**
- Store per-user preferred call actions in `users` table (`preferred_call_mode`, `default_browser_account_id`).
- Store per-campaign default provider account in `campaigns.default_provider_account_id`.

**Files to change**
- `frontend/src/components/tabs/SettingsTab.jsx`
- `frontend/src/components/campaigns/CampaignDetail.jsx`
- `backend/internal/db/users.go`
- `backend/internal/db/campaigns.go`

---

## 2. Backend Architecture

### 2.1 Provider abstraction with credential validation

**Current state**
- Exotel/Twilio/Tata logic is spread across `internal/dial/` and `internal/wshandler/`.
- Campaigns reference credentials indirectly; validation happens late.

**Target state**
- A `Provider` interface and a registry.

```go
package dial

type Provider interface {
    Name() string
    ValidateCredentials(ctx context.Context, creds ProviderCreds) error
    InitiateCall(ctx context.Context, req InitiateCallRequest) (CallSession, error)
    BuildConnectStream(ctx context.Context, call CallSession) (string, error)
    ParseWebhook(r *http.Request) (WebhookEvent, error)
}
```

Implementations:
- `exotelProvider`
- `twilioProvider`
- `tataProvider`
- `browserProvider` (no-op outbound, local media)
- `simWebProvider`

**Files to change**
- `backend/internal/dial/provider.go` (new)
- `backend/internal/dial/exotel.go`
- `backend/internal/dial/twilio.go`
- `backend/internal/dial/tata.go` (existing)
- `backend/internal/db/exotel_accounts.go`

**Validation flow**
1. When provider account is saved, call `ValidateCredentials`.
2. When campaign is saved, ensure `default_provider_account_id` belongs to the org and is valid.
3. When dial is requested, resolve provider by priority:
   - Lead/executive assigned account
   - Campaign default account
   - User default account
   - Org fallback account
4. If no valid account, return HTTP 400 with clear message instead of 502.

### 2.2 Call state machine

**Current state**
- State is implicit in Redis + `wshandler` logic.

**Target state**
- Explicit `CallManager` with states and transitions.

```go
type CallState string
const (
    StatePending     CallState = "pending"
    StateDialing     CallState = "dialing"
    StateConnected   CallState = "connected"
    StateSpeaking    CallState = "speaking"
    StateListening   CallState = "listening"
    StateCompleted   CallState = "completed"
    StateFailed      CallState = "failed"
    StateNoAnswer    CallState = "no_answer"
    StateBusy        CallState = "busy"
)
```

**Files to change**
- `backend/internal/callmanager/` (new package)
- `backend/internal/wshandler/handler.go`
- `backend/internal/wshandler/pipeline.go`
- `backend/internal/wshandler/session.go`

### 2.3 Queue-based auto-dialer

**Current state**
- Auto Dial is driven by frontend/backend loops without a durable queue.

**Target state**
- Redis-backed queue per campaign.
- Worker pool consumes queue, respecting:
  - Provider rate limits
  - TRAI call-hour rules (`internal/callguard`)
  - Agent availability
  - Retry budget

**Queue keys**
```
callified:campaign:{id}:dial_queue          # leads to dial now
callified:campaign:{id}:retry_queue         # failed leads with backoff
callified:campaign:{id}:completed           # terminal outcomes
```

**Files to change**
- `backend/internal/workers/dialer_worker.go` (new)
- `backend/internal/workers/scheduler.go`
- `backend/internal/redis/store.go`
- `backend/internal/dial/initiator.go`

**Auto-dial UX**
- Remove forced Save/Next popup.
- Frontend subscribes to SSE and shows progress (called/remaining/qualified/appointments).
- Allow pause/resume/abort.

### 2.4 WebSocket authentication

**Current state**
- `/media-stream`, `/ws/monitor/{key}`, `/ws/agent`, `/ws/sandbox` are unauthenticated (per security audit).

**Target state**
- Validate JWT or ticket **before** `upgrader.Upgrade`.
- Ticket endpoint generates short-lived WebSocket tickets.
- Reject unknown `stream_sid`/`call_sid`.

**Files to change**
- `backend/internal/api/server.go`
- `backend/internal/wshandler/handler.go`
- `backend/internal/api/auth.go`

### 2.5 Storage abstraction for recordings

**Current state**
- Recording URLs directly reference Oracle bucket paths.
- No health check or signed URLs.

**Target state**
- `RecordingStorage` interface:
  ```go
  type RecordingStorage interface {
      Store(ctx, key string, data io.Reader) (StoredObject, error)
      GetURL(ctx, key string, expiry time.Duration) (string, error)
      HealthCheck(ctx) error
  }
  ```
- Implementations: OCI, S3, local.
- On read, generate pre-signed URL.
- Health-check on startup.

**Files to change**
- `backend/internal/storage/recording.go` (new)
- `backend/internal/storage/oci.go`
- `backend/internal/storage/s3.go`
- `backend/internal/recording/service.go`

### 2.6 Database hardening

**Actions**
- Add composite indexes:
  - `(org_id, campaign_id, status)` on leads
  - `(org_id, executive_id, status)` on leads
  - `(campaign_id, created_at)` on call_logs
  - `(org_id, role)` on users
- Audit all `utf8mb4_unicode_ci` vs `utf8mb4_0900_ai_ci` joins.
- Tune MySQL connection pool in `internal/db/db.go`.

**Files to change**
- `backend/internal/db/db.go`
- `backend/scripts/migrations/` (new migration files)

---

## 3. AI Pipeline

### 3.1 Conversation state management

**Current state**
- Prompt builder has limited memory of what has already happened.
- Greeting can repeat.

**Target state**
- Maintain `ConversationState` per session:
  ```json
  {
    "greetingDone": true,
    "languageLocked": "hi-IN",
    "questionsAsked": ["company_age", "import_history"],
    "lastSpeaker": "ai",
    "turnCount": 3,
    "buyerProfile": "wholesale_distributor"
  }
  ```
- Inject this state into every LLM call.
- Guard against repeating greetings.

**Files to change**
- `backend/internal/prompt/builder.go`
- `backend/internal/wshandler/session.go`
- `backend/internal/llm/provider.go`

### 3.2 Lock language per call

**Current state**
- Language changes dynamically during calls.

**Target state**
- Determine language from campaign settings at call start.
- Lock STT, LLM, TTS to that language.
- Only allow language switching if explicitly enabled for the campaign.

**Files to change**
- `backend/internal/wshandler/handler.go`
- `backend/internal/stt/deepgram.go`
- `backend/internal/tts/` providers

### 3.3 Prompt registry and versioning

**Current state**
- Prompts are hard-coded in `internal/prompt/builder.go`.

**Target state**
- Store prompt templates in DB: `prompt_templates(id, name, version, content, variables, language, product_type)`.
- Campaigns reference `opening_script_id`.
- Support variants like the Panora scripts provided by the user:
  - `panora_v4_curiosity`
  - `panora_wholesale`
  - `panora_hotel_procurement`
  - `panora_retail`

**Files to change**
- `backend/internal/prompt/registry.go` (new)
- `backend/internal/prompt/builder.go`
- `backend/scripts/migrations/20250814_add_prompt_templates.sql`
- Frontend: product creation page should allow selecting/previewing scripts.

### 3.4 LLM output guardrails

**Target state**
- Post-process every LLM output before TTS:
  - Strip repeated greetings if `greetingDone`.
  - Remove hallucinated URLs/phone numbers.
  - Limit response length.
  - Map to allowed intents.

**Files to change**
- `backend/internal/llm/provider.go`
- `backend/internal/wshandler/pipeline.go`

### 3.5 Provider fallback chains

**Target state**
- Configurable fallback per language and campaign.
- TTS: Sarvam (Hindi/Marathi) → ElevenLabs → SmallestAI.
- STT: Deepgram → Groq Whisper.
- LLM: Gemini → Groq → Anthropic.

**Files to change**
- `backend/internal/llm/fallback.go` (new)
- `backend/internal/stt/fallback.go` (new)
- `backend/internal/tts/fallback.go` (new)

### 3.6 Async post-call analysis

**Current state**
- Recording analysis runs synchronously or inline.

**Target state**
- Queue transcript/recording for async analysis.
- Extract: outcome, sentiment, summary, cost, recording URL, qualified, appointment.
- Provide single endpoint: `GET /api/calls/{call_id}/report`.

**Files to change**
- `backend/internal/recording/service.go`
- `backend/internal/workers/analysis_worker.go` (new)
- `backend/internal/llm/analysis.go` (new)

---

## 4. Observability & Reliability

### 4.1 Metrics to add

- [ ] Per-provider dial success/failure rate.
- [ ] Call state transition counts.
- [ ] LLM token usage and latency.
- [ ] STT/TTS latency.
- [ ] Queue depth per campaign.
- [ ] WebSocket connection count and duration.

### 4.2 Circuit breakers

- [ ] If provider returns 401/5xx repeatedly, pause dialing and alert.
- [ ] Auto-recovery after cooldown.

### 4.3 Dead-letter queues

- [ ] Failed webhooks → retry 3 times → DLQ.
- [ ] Failed recording uploads → retry → DLQ.

### 4.4 Tracing

- [ ] Add trace IDs across API → WebSocket → provider webhook → LLM.

---

## 5. Security

### 5.1 Secret management

- [ ] Remove plaintext DB password from `backend/.env`.
- [ ] Use systemd `EnvironmentFile` split or file-based secrets.
- [ ] Rotate MySQL password.

### 5.2 Webhook verification

- [ ] Verify Twilio and Exotel webhook signatures.
- [ ] Remove dev-mode bypasses.

### 5.3 WebSocket auth

- [ ] Authenticate all WebSocket upgrades.

### 5.4 File uploads

- [ ] Store recordings/docs under `org_id/` subdirectories with UUID filenames.
- [ ] Validate upload MIME types and size.

---

## 6. Phased Implementation Roadmap

### Phase 1 — Stability (Weeks 1–2)

#### Backend
- [x] 1.1 Create `dial.Provider` interface with Exotel, Twilio, Tata implementations.
- [x] 1.2 Add credential validation on provider account save.
- [ ] 1.3 Resolve provider account by priority (lead → campaign → user → org fallback).
- [x] 1.4 Return clear 4xx errors instead of 502 for missing/invalid credentials.

#### Storage
- [x] 1.5 Create `storage.RecordingStorage` interface (OCI/S3/local).
- [ ] 1.6 Generate pre-signed recording URLs at read time.
- [ ] 1.7 Add storage health check on startup.

#### Security
- [ ] 1.8 Authenticate WebSocket upgrades before `upgrader.Upgrade`.
- [x] 1.9 Remove plaintext DB password from `backend/.env`; move to file-based secrets.
- [ ] 1.10 Rotate MySQL password.

---

### Phase 2 — Dialer & State (Weeks 3–4)

#### State Management
- [x] 2.1 Define explicit `CallState` enum and `CallManager` package.
- [~] 2.2 Track all call transitions and emit events. (Transitions tracked; event emission pending.)

#### Auto-Dialer
- [x] 2.3 Implement Redis-backed queue per campaign (`campaign:{id}:dial_queue` via global `dial_queue` + per-campaign state).
- [x] 2.4 Add retry queue with exponential backoff.
- [~] 2.5 Respect TRAI call-hour rules (`internal/callguard`). **Disabled by request — callguard always allows.**
- [x] 2.6 Remove forced Save/Next popup from AI auto-dial UX (queue runs uninterrupted; browser auto-dial uninterrupted mode defaults to on).
- [x] 2.7 Add pause/resume/abort controls.

#### Language Stability
- [ ] 2.8 Lock language from campaign settings at call start.
- [ ] 2.9 Enforce locked language across STT/LLM/TTS.

#### Conversation State
- [~] 2.10 Add `ConversationState` per session (`greetingDone`, `questionsAsked`, etc.). (`greetingDone` guard implemented; full struct pending.)
- [x] 2.11 Guard against repeated greetings.

---

### Phase 3 — Frontend Modernization (Weeks 5–6)

#### State Layer
- [x] 3.1 Add `@tanstack/react-query` dependency.
- [x] 3.2 Create query hooks: `useCampaigns`, `useCampaign`, `useLeads`, `useCallLogs`, `useAgentReport`.
- [x] 3.3 Replace ad-hoc `useEffect` fetches in key pages.
- [x] 3.4 Mutations invalidate related query keys.

#### Real-Time
- [x] 3.5 Implement SSE endpoint `/api/events` in backend.
- [x] 3.6 Create `EventContext` in frontend.
- [x] 3.7 Push events for lead status, call completion, agent presence.
- [x] 3.8 Extend events to agent presence and other mutations (CRM, products, billing).

#### Routing
- [x] 3.9 Create central `routeConfig.js` with permissions and AI-feature flags.
- [x] 3.10 Create `ProtectedRoute` wrapper.
- [x] 3.11 Replace scattered `hideAiFeatures ? <Navigate to="/crm" />` in `App.jsx`.

#### Performance
- [x] 3.12 Lazy-load Analytics, Agent Report, Team, User Management, Receptionist.
- [x] 3.13 Reduce initial bundle below 500 KB.

---

### Phase 4 — AI & Analytics (Weeks 7–8)

#### Prompt Registry
- [ ] 4.1 Add `prompt_templates` table with versioning.
- [ ] 4.2 Migrate existing hard-coded prompts into registry.
- [ ] 4.3 Add Panora script variants (`panora_v4_curiosity`, `panora_wholesale`, etc.).
- [ ] 4.4 Allow script selection per campaign/product.

#### AI Guardrails
- [ ] 4.5 Post-process LLM output before TTS.
- [ ] 4.6 Strip repeated greetings and hallucinated URLs.
- [ ] 4.7 Map outputs to allowed intents.

#### Provider Fallbacks
- [ ] 4.8 Implement TTS fallback chain (Sarvam → ElevenLabs → SmallestAI).
- [ ] 4.9 Implement STT fallback chain (Deepgram → Groq Whisper).
- [ ] 4.10 Implement LLM fallback chain (Gemini → Groq → Anthropic).

#### Post-Call Analysis
- [ ] 4.11 Queue recording/transcript for async analysis.
- [ ] 4.12 Extract outcome, sentiment, summary, cost, recording URL, qualified, appointment.
- [ ] 4.13 Add `GET /api/calls/{call_id}/report`.

#### Access & Data Quality
- [ ] 4.14 Implement agent-specific lead visibility and call ownership.
- [ ] 4.15 Add duplicate phone filtering in campaign lead lists.

---

### Phase 5 — Scale & Observability (Weeks 9–10)

#### Metrics
- [ ] 5.1 Add per-provider dial success/failure metrics.
- [ ] 5.2 Track call state transitions.
- [ ] 5.3 Track LLM token usage and latency.
- [ ] 5.4 Track STT/TTS latency and queue depth.

#### Reliability
- [ ] 5.5 Add circuit breakers for providers.
- [ ] 5.6 Add dead-letter queues for failed webhooks and uploads.
- [ ] 5.7 Add trace IDs across API → WebSocket → webhook → LLM.

#### Database
- [ ] 5.8 Add indexes for lead/campaign/agent filtering.
- [ ] 5.9 Audit collation mismatches.
- [ ] 5.10 Tune MySQL connection pool.

#### Architecture
- [ ] 5.11 Evaluate splitting monolith into API / dialer / worker services.
- [ ] 5.12 Design inter-service event bus if split proceeds.

---

## 7. Success Metrics

| Metric | Current | Target |
|--------|---------|--------|
| AI dial failure rate | High (401/502) | < 2% |
| Auto-dial interruptions | Forced popups | Zero interruptions |
| Dashboard refresh delay | Manual refresh | < 3 seconds via SSE |
| Repeated greeting rate | Observable | Zero |
| Duplicate leads in campaign list | Present | Zero |
| WebSocket auth coverage | None | 100% |
| Bundle size | 862 KB | < 500 KB initial |
| MySQL query latency p95 | > 500 ms | < 100 ms |

---

## 8. Risk Mitigation

| Risk | Mitigation |
|------|------------|
| Large refactor breaks dialer | Incremental changes behind feature flags; A/B test on testgo1/testgo2 first. |
| Provider credential rotation causes outages | Validate new credentials before switching; keep old credentials as fallback during transition. |
| DB migrations lock tables | Run migrations during low-traffic windows; use `pt-online-schema-change` if needed. |
| SSE load on backend | Use Redis pub/sub to fan out events; one SSE connection per user. |
| Prompt registry confuses existing campaigns | Keep default prompt backward-compatible; migration sets `opening_script_id` for existing campaigns. |

---

## 9. Suggested Next Immediate Actions

- [ ] **1. Provider abstraction + credential validation** — highest ROI; fixes live Exotel failures.
- [ ] **2. Conversation state + locked language** — fixes greeting/language issues immediately.
- [ ] **3. Queue-based auto-dialer** — enables uninterrupted auto-dial.
- [ ] **4. React Query + SSE** — modernizes frontend and fixes stale dashboard.
- [ ] **5. Prompt registry + Panora scripts** — makes scripts configurable per campaign.

Would you like me to start implementing any of these phases, or create smaller PR-sized tickets for a specific phase?
