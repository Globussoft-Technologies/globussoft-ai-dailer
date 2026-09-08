# Call Memory — Cross-Call Memory for the Voice Agent

> **Origin:** Gap #3 (Memory) from the audit of the Callified stack vs Gaurav Sharma's (SaaS Labs) Voice AI post — cross-call persistent memory was our biggest gap.
>
> **Status:** Proposal — awaiting approval.

---

## 1. Problem

Today every call starts from zero. The agent re-pitches leads it already spoke to, forgets commitments, and repeats mistakes — because it has no memory of past calls with the same person.

Meanwhile, after every call we already pay Gemini to write a review (summary, failure reason, improvement suggestion) into `call_reviews` — and nothing ever reads it back. **The memory is generated and thrown away.**

### Gap vs. the "multi-type agent memory" model

| Memory type | Callified today |
|---|---|
| Per-call memory | Full in-memory history, no trimming, dies at hangup |
| Cross-call persistent | **Doesn't exist for voice** — Gemini call summaries go to `call_reviews` but nothing feeds them back into future prompts |
| Customer/account knowledge | Lead name + source only. RAG KB exists but the voice pipeline never calls it — only WhatsApp does |
| Intentional forgetting | None |

---

## 2. Solution (v1)

When a call starts, fetch the **last 3 call reviews** for that lead and inject a compact `## PREVIOUS CALLS` block into the agent's system prompt (date + summary + failure reason + improvement suggestion).

Closes the loop using data we already generate — **no new infra, no new services.**

---

## 3. How it works, step by step

1. At call start (`BuildCallContext`), query DB: "last 3 call_reviews for this lead" (join `call_reviews` → `call_transcripts` on `lead_id`).
2. Render a compact block per past call: date, summary, failure reason, and the AI's own `prompt_improvement_suggestion`. Skipped entirely if the lead was never called — zero change to existing behavior.
3. Block lands as a `## PREVIOUS CALLS` section after PRODUCT KNOWLEDGE — works for both the default prompt and org custom prompts.
4. Small data fix: `SaveCallReview` doesn't store `lead_id` today — add the column (1 migration line) and populate it on save. Without this we can't look up "past calls for this lead" reliably.

---

## 4. In plain words

**Today:** Every call is like meeting a stranger. The agent says "Hello, I'm calling about X" even if it called the same person yesterday and got rejected.

**After this fix:** The agent remembers.

1. You call a lead. Call ends. Our system already writes a short note about that call — like *"customer said price too high, asked to call next week."* (This note already exists today; it's just never used.)
2. Next time you call that same lead, before the call starts, the system reads that note and puts it into the agent's instructions: *"You spoke to this person before. They rejected because of price. They asked for a callback."*
3. So the agent starts the call already knowing the history — no repeated pitch, no awkward "have we met?" moments.

**That's it. Call ends → note is saved → next call reads the note.**

The only new things we build:

- A small DB lookup: *"get me the last notes for this lead"* when a call starts
- Saving the lead's ID with each note (so we can find notes by lead)

Nothing else changes. Calls without history work exactly like today.

---

## 5. Flow

```
                    ┌─────────────────────────────────────────────┐
                    │            CALL N (today)                   │
                    │                                             │
  Outbound dial     │  1. Dial lead ──► WS /media-stream connects │
  starts     ──────►│                                             │
                    │  2. BuildCallContext(orgID, campaignID,     │
                    │     leadID, language)                       │
                    │        │                                    │
                    │        ▼                                    │
                    │     ┌──────────────────────────┐            │
                    │     │ DB query (NEW):          │            │
                    │     │ "last 3 call_reviews     │            │
                    │     │  for this lead"          │            │
                    │     └──────────┬───────────────┘            │
                    │        │ reviews found?                     │
                    │        ▼                                    │
                    │     Render "## PREVIOUS CALLS" block        │
                    │     (date + summary + failure_reason)       │
                    │        │                                    │
                    │        ▼                                    │
                    │  3. System prompt = default/custom prompt   │
                    │     + PRODUCT KNOWLEDGE + PREVIOUS CALLS    │
                    │        │                                    │
                    │        ▼                                    │
                    │  4. Greeting TTS ──► conversation runs      │
                    │     with full memory of past calls          │
                    │        │                                    │
                    │        ▼                                    │
                    │  5. Call ends ──► SaveAndAnalyze            │
                    │     ┌──────────────────────────┐            │
                    │     │ Gemini writes review:    │            │
                    │     │ summary, failure_reason, │            │
                    │     │ prompt_improvement...    │            │
                    │     └──────────┬───────────────┘            │
                    │        │                                    │
                    │        ▼                                    │
                    │  6. SaveCallReview ──► call_reviews row     │
                    │     (NOW with lead_id populated ──► this    │
                    │      row becomes memory for CALL N+1)       │
                    └─────────────────────────────────────────────┘
                                      │
                                      ▼  next dial to same lead
                    ┌─────────────────────────────────────────────┐
                    │            CALL N+1                         │
                    │  Step 2 finds the review from CALL N        │
                    │  Agent already knows what happened          │
                    └─────────────────────────────────────────────┘
```

### Data flow (which file touches what)

```
dial/initiator.go              ──► starts call for lead
wshandler/handler.go:1100      ──► calls BuildCallContext(leadID)
prompt/builder.go              ──► NEW: GetLastCallMemory(leadID) + render block
db/reviews.go                  ──► NEW: GetLastCallMemory query method
recording/service.go:243       ──► SaveCallReview (NOW with LeadID set)
db/reviews.go SaveCallReview   ──► INSERT includes lead_id
backend/scripts/migrations/    ──► NEW: add lead_id column to call_reviews
```

---

## 6. Design decisions

- **Use existing Gemini reviews; don't build a new memory system.** The summaries already exist and are already LLM-written. A new vector-memory layer would be months of work; this is days.
- **Cap at 3 past calls + ~200 chars per field.** System prompt bloat = slower + costlier LLM calls. Short, dense memory beats long transcripts.
- **Works with custom org prompts too** — the block is appended after the custom prompt, protected by the existing "never speak internal notes" rule (Rule 16 in the default prompt).
- **Soft failure:** if the DB query fails or the lead is new, the call proceeds exactly as today. Memory never blocks a call.

---

## 7. Edge cases

| Case | Behavior |
|---|---|
| First call to a lead | Memory query returns nothing → prompt unchanged → exactly today's behavior |
| DB down / query fails | Soft fail, call proceeds with no memory block |
| Custom org prompt | Memory block appended after it, covered by the "never speak internal notes" rule |
| No reviews yet for that lead (e.g., calls under 10s skip analysis) | Same as first call |

---

## 8. Risk & rollback

- **Low risk** — memory lookup is soft-fail; if anything errors, calls run as today. Nothing new blocks a dial.
- **Rollback = revert PR**; old behavior restored, no data migration needed to undo.
- **Behavioral note:** the agent will reference past conversations — this is the intended feature, but worth flagging to sales/support teams.

---

## 9. Effort

~1–2 days including tests. No new services, no new API keys, no frontend changes.

---

## 10. Testing plan

- Unit test: lead with past reviews → prompt contains the block; new lead → prompt unchanged
- Manual staging: call same lead twice, verify 2nd call's agent acknowledges the 1st call
- Verify no latency added to call start (one indexed lookup)

---

## 11. Out of scope (v1)

- No mid-call memory (injected only at call start)
- No RAG knowledge-base in voice path (separate task)
- Old reviews before this ships have no `lead_id` — optional one-time backfill later, not required
- No automatic memory expiry; the 3-call cap handles it implicitly

---

## 12. Future (post-v1)

- **RAG in the voice path** — knowledge-base retrieval during calls (currently WhatsApp-only)
- **Outcome-based billing hook** — `call_reviews.appointment_booked` already exists and is LLM-verified; a billing rule in `DeductCallCredits` is the natural next step
- **Backfill** `lead_id` on historical reviews with a one-time UPDATE so old calls also become memory
