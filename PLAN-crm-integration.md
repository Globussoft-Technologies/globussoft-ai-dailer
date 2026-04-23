# Globus CRM Integration — Callified Go Backend

## Context

Globus CRM (`crm.globusdemos.com`) is a Globussoft-built Node.js CRM running on the same server (port 5099).
The goal is to let Callified dial leads from a campaign and automatically push call outcomes (interest level,
sentiment, appointment status, call duration) back into the CRM contact record after every call completes.

The backend already has a full CRM integration framework (DB table `crm_integrations`, poller worker,
`/api/integrations` CRUD endpoints) used for Pipedrive/HubSpot/Salesforce/Zoho. This plan reuses that
infrastructure and adds Globus CRM as a new push-direction provider.

---

## Architecture

```
Call ends (WebSocket close)
  └── wshandler.finalizeCall()
        └── go recordingSvc.SaveAndAnalyze()   [recording/service.go]
              ├── SaveCallTranscript()
              ├── analyzeCall() → Gemini
              ├── SaveCallReview()              ← outcomes available here
              ├── auto-DND if needed
              ├── Dispatch call.completed webhook
              ├── WA appointment confirmation
              └── ➕ NEW: go s.pushToCRM()     ← Step 9 (fire-and-forget)
                        └── crm/push.go
                              ├── GetCRMIntegrationByOrgProvider(orgID, "globuscrm")
                              ├── Login(url, username, password) → JWT
                              ├── FindContactByPhone(phone) → contactID
                              ├── UpdateContactStatus(contactID, status)
                              └── CreateCallLog(contactID, duration, summary)
```

---

## Sentiment → CRM Status Mapping

| Call outcome | CRM `Contact.status` |
|---|---|
| `appointment_booked = true` | `"Customer"` |
| `sentiment = "positive"` | `"Interested"` |
| `sentiment = "negative"` | `"Not Interested"` |
| `sentiment = "neutral"` | `"Prospect"` |

---

## Step-by-Step Implementation

### Step 1 — DB: add `GetCRMIntegrationByOrgProvider()`

**File:** `backend/internal/db/integrations.go`

Add one new method below the existing ones. Reuses the same scan pattern as `GetActiveCRMIntegrations()`:

```go
// GetCRMIntegrationByOrgProvider returns the active integration for a specific org+provider.
// Returns nil when none found.
func (d *DB) GetCRMIntegrationByOrgProvider(orgID int64, provider string) (*CRMIntegration, error) {
    row := d.pool.QueryRow(`
        SELECT id, org_id, provider, COALESCE(credentials,'{}'), COALESCE(is_active,1),
               COALESCE(DATE_FORMAT(last_synced_at,'%Y-%m-%d %H:%i:%s'),''),
               DATE_FORMAT(created_at,'%Y-%m-%d %H:%i:%s')
        FROM crm_integrations WHERE org_id=? AND provider=? AND is_active=1`, orgID, provider)
    var ci CRMIntegration
    var credsJSON string
    var active int
    err := row.Scan(&ci.ID, &ci.OrgID, &ci.Provider, &credsJSON, &active, &ci.LastSyncedAt, &ci.CreatedAt)
    if errors.Is(err, sql.ErrNoRows) { return nil, nil }
    if err != nil { return nil, err }
    ci.IsActive = active == 1
    json.Unmarshal([]byte(credsJSON), &ci.Credentials) //nolint:errcheck
    return &ci, nil
}
```

---

### Step 2 — New File: `backend/internal/crm/push.go`

Create the Globus CRM HTTP client. Credentials stored in `crm_integrations.credentials`:
```json
{ "url": "https://crm.globusdemos.com", "username": "admin@example.com", "password": "secret" }
```

**Struct and methods:**

```go
package crm

// Client authenticates with Globus CRM and pushes call outcomes.
// It caches the JWT in memory and re-logins when expired.
type Client struct {
    url      string
    username string
    password string

    mu       sync.Mutex
    token    string
    tokenExp time.Time
    http     *http.Client
}

func NewClient(url, username, password string) *Client

// ensureToken() — if token expired or empty: POST /api/auth/login → cache JWT for 23 hours
func (c *Client) ensureToken(ctx context.Context) error

// FindContactByPhone — GET /api/contacts with phone filter, returns contactID (0 if not found)
func (c *Client) FindContactByPhone(ctx context.Context, phone string) (int, error)

// UpdateContactStatus — PUT /api/contacts/:id  { "status": "..." }
func (c *Client) UpdateContactStatus(ctx context.Context, contactID int, status string) error

// CreateCallLog — POST /api/telephony/call-logs
// { "contactId": int, "duration": int, "notes": string, "direction": "OUTBOUND", "status": "COMPLETED" }
func (c *Client) CreateCallLog(ctx context.Context, contactID int, durationS float32, notes string) error

// PushCallOutcome — top-level helper: FindContactByPhone → UpdateContactStatus → CreateCallLog
// Returns early without error if contact not found (not all leads may exist in CRM)
func (c *Client) PushCallOutcome(ctx context.Context, phone, status string, durationS float32, summary string) error
```

**Auth endpoint:** `POST /api/auth/login` → `{ "email": "...", "password": "..." }` → `{ "token": "eyJ..." }`
All calls: `Authorization: Bearer <token>`.

---

### Step 3 — Modify `recording/service.go` — add step 9

**File:** `backend/internal/recording/service.go`

After existing step 8 (WA confirmation, line ~128), add:

```go
// 9. Push outcome to Globus CRM if connected for this org (fire-and-forget).
go s.pushToCRM(context.Background(), req.OrgID, req.LeadPhone, req.DurationS, review)
```

Add `pushToCRM()` method on `*Service`:

```go
func (s *Service) pushToCRM(ctx context.Context, orgID int64, phone string, durationS float32, review *db.CallReview) {
    integ, err := s.database.GetCRMIntegrationByOrgProvider(orgID, "globuscrm")
    if err != nil {
        s.log.Warn("recording: CRM lookup failed", zap.Error(err))
        return
    }
    if integ == nil { return } // not configured — skip silently

    client := crm.NewClient(integ.Credentials["url"], integ.Credentials["username"], integ.Credentials["password"])

    status := "Prospect"
    if review.AppointmentBooked {
        status = "Customer"
    } else if review.Sentiment == "positive" {
        status = "Interested"
    } else if review.Sentiment == "negative" {
        status = "Not Interested"
    }

    if err := client.PushCallOutcome(ctx, phone, status, durationS, review.Summary); err != nil {
        s.log.Warn("recording: CRM push failed", zap.Error(err))
    } else {
        s.log.Info("recording: CRM push ok", zap.String("phone", phone), zap.String("status", status))
    }
}
```

Add import: `"github.com/globussoft/callified-backend/internal/crm"`

---

### Step 4 — API: `POST /api/crm-integrations` + `GET /api/crm-integrations`

**File:** `backend/internal/api/integrations.go`

```go
// POST /api/crm-integrations — save Globus CRM connection
func (s *Server) saveCRMIntegration(w http.ResponseWriter, r *http.Request) {
    var body struct {
        URL      string `json:"url"`
        Username string `json:"username"`
        Password string `json:"password"`
    }
    if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.URL == "" || body.Username == "" {
        writeError(w, http.StatusBadRequest, "url, username, password required")
        return
    }
    ac := getAuth(r)
    id, err := s.db.SaveCRMIntegration(ac.OrgID, "globuscrm",
        map[string]string{"url": body.URL, "username": body.Username, "password": body.Password})
    if err != nil {
        writeError(w, http.StatusInternalServerError, "internal error")
        return
    }
    writeJSON(w, http.StatusOK, map[string]any{"id": id})
}

// GET /api/crm-integrations — check connection status (omits password)
func (s *Server) getCRMIntegration(w http.ResponseWriter, r *http.Request) {
    ac := getAuth(r)
    integ, err := s.db.GetCRMIntegrationByOrgProvider(ac.OrgID, "globuscrm")
    if err != nil {
        writeError(w, http.StatusInternalServerError, "internal error")
        return
    }
    if integ == nil {
        writeJSON(w, http.StatusOK, map[string]any{"connected": false})
        return
    }
    writeJSON(w, http.StatusOK, map[string]any{
        "connected": true,
        "id":        integ.ID,
        "url":       integ.Credentials["url"],
        "username":  integ.Credentials["username"],
    })
}

// DELETE /api/crm-integrations — disconnect
func (s *Server) deleteCRMIntegration(w http.ResponseWriter, r *http.Request) {
    // reuse existing deleteIntegration pattern with provider="globuscrm"
}
```

---

### Step 5 — Register routes in `server.go`

**File:** `backend/internal/api/server.go`

Add alongside existing CRM integration routes (~line 280):

```go
mux.HandleFunc("POST   /api/crm-integrations", auth(s.saveCRMIntegration))
mux.HandleFunc("GET    /api/crm-integrations", auth(s.getCRMIntegration))
mux.HandleFunc("DELETE /api/crm-integrations", auth(s.deleteCRMIntegration))
```

---

## Critical Files

| File | Change |
|------|--------|
| `backend/internal/db/integrations.go` | Add `GetCRMIntegrationByOrgProvider()` |
| `backend/internal/crm/push.go` | **NEW** — Globus CRM HTTP client |
| `backend/internal/recording/service.go` | Add step 9 + `pushToCRM()` method |
| `backend/internal/api/integrations.go` | Add 3 new handlers |
| `backend/internal/api/server.go` | Register 3 new routes |

**Reused unchanged:**
- `db.SaveCRMIntegration()` — already upserts by org+provider key
- `db.CRMIntegration` struct + `crm_integrations` table — correct shape
- `recording.Service.database` — already has `*db.DB`

---

## Verification

1. `POST /api/crm-integrations` `{url, username, password}` → `{"id": N}`
2. `GET /api/crm-integrations` → `{"connected": true, "url": "...", "username": "..."}`
3. Trigger a sandbox call to a phone that exists as a Contact in Globus CRM
4. After call ends: open CRM contact — status updated, call log entry created
5. Server logs: `sudo journalctl -u callified-go-audio -n 50` → `recording: CRM push ok`
6. `go build ./...` — clean build, no errors
