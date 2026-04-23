package workers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"go.uber.org/zap"

	"github.com/globussoft/callified-backend/internal/db"
)

// CRMPoller polls registered CRM integrations every 60 seconds for new leads.
type CRMPoller struct {
	db     *db.DB
	client *http.Client
	log    *zap.Logger
}

// NewCRMPoller creates a CRMPoller.
func NewCRMPoller(database *db.DB, log *zap.Logger) *CRMPoller {
	return &CRMPoller{
		db:     database,
		client: &http.Client{Timeout: 20 * time.Second},
		log:    log,
	}
}

// Run starts the CRM polling loop. Blocks until ctx is cancelled.
func (p *CRMPoller) Run(ctx context.Context) {
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()
	p.log.Info("crm_poller: started")
	for {
		select {
		case <-ctx.Done():
			p.log.Info("crm_poller: stopped")
			return
		case <-ticker.C:
			p.tick(ctx)
		}
	}
}

func (p *CRMPoller) tick(ctx context.Context) {
	integrations, err := p.db.GetActiveCRMIntegrations()
	if err != nil {
		p.log.Warn("crm_poller: GetActiveCRMIntegrations", zap.Error(err))
		return
	}
	for _, integ := range integrations {
		if ctx.Err() != nil {
			return
		}
		count, err := p.pollIntegration(ctx, integ)
		if err != nil {
			p.log.Warn("crm_poller: poll failed",
				zap.String("provider", integ.Provider), zap.Error(err))
			continue
		}
		if count > 0 {
			p.log.Info("crm_poller: synced leads",
				zap.String("provider", integ.Provider),
				zap.Int64("org_id", integ.OrgID),
				zap.Int("count", count))
		}
		_ = p.db.UpdateCRMLastSynced(integ.ID)
	}
}

// pollIntegration fetches new leads from one CRM and upserts them. Returns count created.
func (p *CRMPoller) pollIntegration(ctx context.Context, integ db.CRMIntegration) (int, error) {
	var leads []crmLead
	var err error
	switch integ.Provider {
	case "pipedrive":
		leads, err = p.fetchPipedrive(ctx, integ.Credentials)
	case "hubspot":
		leads, err = p.fetchHubspot(ctx, integ.Credentials)
	case "salesforce":
		leads, err = p.fetchSalesforce(ctx, integ.Credentials)
	case "zoho":
		leads, err = p.fetchZoho(ctx, integ.Credentials)
	default:
		return 0, fmt.Errorf("unknown CRM provider: %s", integ.Provider)
	}
	if err != nil {
		return 0, err
	}

	created := 0
	for _, cl := range leads {
		// Skip if already imported
		existing, _ := p.db.GetLeadByExternalID(cl.ExternalID, integ.Provider, integ.OrgID)
		if existing != nil {
			continue
		}
		id, err := p.db.CreateLead(cl.FirstName, cl.LastName, cl.Phone, integ.Provider, cl.Interest, integ.OrgID)
		if err != nil {
			p.log.Warn("crm_poller: CreateLead", zap.Error(err))
			continue
		}
		// Tag with external_id + crm_provider
		_, _ = p.db.UpdateLead(id, cl.FirstName, cl.LastName, cl.Phone, integ.Provider, cl.Interest, integ.OrgID)
		created++
	}
	return created, nil
}

type crmLead struct {
	ExternalID string
	FirstName  string
	LastName   string
	Phone      string
	Interest   string
}

// ─── Provider fetch helpers ────────────────────────────────────────────────────

func (p *CRMPoller) fetchPipedrive(ctx context.Context, creds map[string]string) ([]crmLead, error) {
	apiToken := creds["api_token"]
	url := fmt.Sprintf("https://api.pipedrive.com/v1/persons?api_token=%s&limit=100", apiToken)
	body, err := p.getJSON(ctx, url, "")
	if err != nil {
		return nil, err
	}
	// Pipedrive: {"data":[{"id":1,"name":"John Doe","phones":[{"value":"..."}]}]}
	var resp struct {
		Data []struct {
			ID    int    `json:"id"`
			Name  string `json:"name"`
			Phone []struct {
				Value string `json:"value"`
			} `json:"phones"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, err
	}
	var leads []crmLead
	for _, d := range resp.Data {
		phone := ""
		if len(d.Phone) > 0 {
			phone = d.Phone[0].Value
		}
		parts := splitName(d.Name)
		leads = append(leads, crmLead{
			ExternalID: fmt.Sprintf("%d", d.ID),
			FirstName:  parts[0],
			LastName:   parts[1],
			Phone:      phone,
		})
	}
	return leads, nil
}

func (p *CRMPoller) fetchHubspot(ctx context.Context, creds map[string]string) ([]crmLead, error) {
	accessToken := creds["access_token"]
	url := "https://api.hubapi.com/crm/v3/objects/contacts?limit=100&properties=firstname,lastname,phone"
	body, err := p.getJSON(ctx, url, "Bearer "+accessToken)
	if err != nil {
		return nil, err
	}
	// HubSpot: {"results":[{"id":"...","properties":{"firstname":"","lastname":"","phone":""}}]}
	var resp struct {
		Results []struct {
			ID         string `json:"id"`
			Properties struct {
				FirstName string `json:"firstname"`
				LastName  string `json:"lastname"`
				Phone     string `json:"phone"`
			} `json:"properties"`
		} `json:"results"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, err
	}
	var leads []crmLead
	for _, r := range resp.Results {
		leads = append(leads, crmLead{
			ExternalID: r.ID,
			FirstName:  r.Properties.FirstName,
			LastName:   r.Properties.LastName,
			Phone:      r.Properties.Phone,
		})
	}
	return leads, nil
}

func (p *CRMPoller) fetchSalesforce(ctx context.Context, creds map[string]string) ([]crmLead, error) {
	instanceURL := creds["instance_url"]
	accessToken := creds["access_token"]
	apiVersion := "v58.0"
	if v := creds["api_version"]; v != "" {
		apiVersion = v
	}
	url := fmt.Sprintf("%s/services/data/%s/query?q=SELECT+Id,FirstName,LastName,Phone+FROM+Lead+LIMIT+100",
		instanceURL, apiVersion)
	body, err := p.getJSON(ctx, url, "Bearer "+accessToken)
	if err != nil {
		return nil, err
	}
	// Salesforce: {"records":[{"Id":"...","FirstName":"","LastName":"","Phone":""}]}
	var resp struct {
		Records []struct {
			ID        string `json:"Id"`
			FirstName string `json:"FirstName"`
			LastName  string `json:"LastName"`
			Phone     string `json:"Phone"`
		} `json:"records"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, err
	}
	var leads []crmLead
	for _, r := range resp.Records {
		leads = append(leads, crmLead{
			ExternalID: r.ID,
			FirstName:  r.FirstName,
			LastName:   r.LastName,
			Phone:      r.Phone,
		})
	}
	return leads, nil
}

func (p *CRMPoller) fetchZoho(ctx context.Context, creds map[string]string) ([]crmLead, error) {
	accessToken := creds["access_token"]
	url := "https://www.zohoapis.in/crm/v2/Leads?fields=First_Name,Last_Name,Phone&per_page=100"
	body, err := p.getJSON(ctx, url, "Zoho-oauthtoken "+accessToken)
	if err != nil {
		return nil, err
	}
	// Zoho: {"data":[{"id":"...","First_Name":"","Last_Name":"","Phone":""}]}
	var resp struct {
		Data []struct {
			ID        string `json:"id"`
			FirstName string `json:"First_Name"`
			LastName  string `json:"Last_Name"`
			Phone     string `json:"Phone"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, err
	}
	var leads []crmLead
	for _, d := range resp.Data {
		leads = append(leads, crmLead{
			ExternalID: d.ID,
			FirstName:  d.FirstName,
			LastName:   d.LastName,
			Phone:      d.Phone,
		})
	}
	return leads, nil
}

func (p *CRMPoller) getJSON(ctx context.Context, url, authHeader string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	if authHeader != "" {
		req.Header.Set("Authorization", authHeader)
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("HTTP %d from %s", resp.StatusCode, url)
	}
	return io.ReadAll(resp.Body)
}

func splitName(name string) [2]string {
	for i, c := range name {
		if c == ' ' {
			return [2]string{name[:i], name[i+1:]}
		}
	}
	return [2]string{name, ""}
}
