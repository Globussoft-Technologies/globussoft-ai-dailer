package db

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// VoiceSettings holds TTS configuration for an org or campaign.
type VoiceSettings struct {
	TTSProvider            string `json:"tts_provider"`
	TTSVoiceID             string `json:"tts_voice_id"`
	TTSLanguage            string `json:"tts_language"`
	MaxCallDurationSeconds int    `json:"max_call_duration_seconds"`
}

// Organization mirrors the organizations table.
type Organization struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	Domain    string `json:"domain,omitempty"`
	Timezone  string `json:"timezone"`
	CreatedAt string `json:"created_at"`
}

// EnsureOrganizationsTable makes sure the organizations table exists and adds
// the optional domain column for SSO email-domain matching.
func (d *DB) EnsureOrganizationsTable() error {
	_, err := d.pool.Exec(`
		CREATE TABLE IF NOT EXISTS organizations (
			id INT AUTO_INCREMENT PRIMARY KEY,
			name VARCHAR(255) NOT NULL,
			created_at TIMESTAMP NULL DEFAULT CURRENT_TIMESTAMP,
			custom_system_prompt TEXT DEFAULT NULL,
			tts_provider VARCHAR(50) DEFAULT 'elevenlabs',
			tts_voice_id VARCHAR(100) DEFAULT NULL,
			tts_language VARCHAR(10) DEFAULT 'hi',
			max_call_duration_seconds INT DEFAULT NULL,
			timezone VARCHAR(100) DEFAULT 'Asia/Kolkata',
			onboarding_completed TINYINT(1) DEFAULT 0
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`)
	if err != nil {
		return err
	}
	_, err = d.pool.Exec(`ALTER TABLE organizations ADD COLUMN domain VARCHAR(255) DEFAULT NULL UNIQUE`)
	if err != nil && !strings.Contains(err.Error(), "Duplicate column name") && !strings.Contains(err.Error(), "1061") {
		return err
	}
	_, err = d.pool.Exec(`ALTER TABLE organizations ADD COLUMN max_call_duration_seconds INT DEFAULT NULL`)
	if err != nil && !strings.Contains(err.Error(), "Duplicate column name") {
		return err
	}
	return nil
}

// GetAllOrganizations returns all orgs ordered by id DESC.
func (d *DB) GetAllOrganizations() ([]Organization, error) {
	rows, err := d.pool.Query(
		`SELECT id, name, COALESCE(timezone,'Asia/Kolkata'), DATE_FORMAT(created_at,'%Y-%m-%d %H:%i:%s')
		FROM organizations ORDER BY id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []Organization
	for rows.Next() {
		var o Organization
		if err := rows.Scan(&o.ID, &o.Name, &o.Timezone, &o.CreatedAt); err != nil {
			return nil, err
		}
		list = append(list, o)
	}
	return list, rows.Err()
}

// GetOrganizationByID returns one org by its primary key. Returns nil when not found.
func (d *DB) GetOrganizationByID(id int64) (*Organization, error) {
	row := d.pool.QueryRow(
		`SELECT id, name, COALESCE(domain,''), COALESCE(timezone,'Asia/Kolkata'), DATE_FORMAT(created_at,'%Y-%m-%d %H:%i:%s')
		FROM organizations WHERE id=? LIMIT 1`, id)
	var o Organization
	err := row.Scan(&o.ID, &o.Name, &o.Domain, &o.Timezone, &o.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &o, err
}

// GetOrganizationByDomain looks up an org by its verified email domain.
// Returns nil when no org has that domain.
func (d *DB) GetOrganizationByDomain(domain string) (*Organization, error) {
	row := d.pool.QueryRow(
		`SELECT id, name, COALESCE(domain,''), COALESCE(timezone,'Asia/Kolkata'), DATE_FORMAT(created_at,'%Y-%m-%d %H:%i:%s')
		FROM organizations WHERE domain = ? LIMIT 1`, strings.ToLower(domain))
	var o Organization
	err := row.Scan(&o.ID, &o.Name, &o.Domain, &o.Timezone, &o.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &o, err
}

// CreateOrganizationWithDomain inserts a new org with an optional verified domain.
func (d *DB) CreateOrganizationWithDomain(name, domain string) (int64, error) {
	domain = strings.ToLower(strings.TrimSpace(domain))
	res, err := d.pool.Exec(
		`INSERT INTO organizations (name, domain) VALUES (?, NULLIF(?, ''))`,
		name, domain)
	if err != nil {
		return 0, fmt.Errorf("CreateOrganizationWithDomain: %w", err)
	}
	return res.LastInsertId()
}

// DeleteOrganization deletes an org (cascades to campaigns, leads, users).
func (d *DB) DeleteOrganization(orgID int64) error {
	_, err := d.pool.Exec(`DELETE FROM organizations WHERE id=?`, orgID)
	return err
}

// GetOrgTimezone returns the timezone for an org (defaults to "Asia/Kolkata").
func (d *DB) GetOrgTimezone(orgID int64) (string, error) {
	var tz string
	err := d.pool.QueryRow(
		`SELECT COALESCE(timezone,'Asia/Kolkata') FROM organizations WHERE id=?`, orgID,
	).Scan(&tz)
	if errors.Is(err, sql.ErrNoRows) {
		return "Asia/Kolkata", nil
	}
	return tz, err
}

// IsOnboardingCompleted returns true if onboarding_completed is set for the org.
func (d *DB) IsOnboardingCompleted(orgID int64) (bool, error) {
	var v int
	err := d.pool.QueryRow(
		`SELECT COALESCE(onboarding_completed,0) FROM organizations WHERE id=?`, orgID,
	).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return v == 1, err
}

// MarkOnboardingCompleted sets onboarding_completed=1 for an org.
func (d *DB) MarkOnboardingCompleted(orgID int64) error {
	_, err := d.pool.Exec(
		`UPDATE organizations SET onboarding_completed=1 WHERE id=?`, orgID)
	return err
}

// GetOrgSystemPrompt returns the custom_system_prompt column for an org.
func (d *DB) GetOrgSystemPrompt(orgID int64) (string, error) {
	var prompt string
	err := d.pool.QueryRow(
		`SELECT COALESCE(custom_system_prompt,'') FROM organizations WHERE id=?`, orgID,
	).Scan(&prompt)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return prompt, err
}

// SaveOrgSystemPrompt updates the custom_system_prompt column for an org.
func (d *DB) SaveOrgSystemPrompt(orgID int64, prompt string) error {
	_, err := d.pool.Exec(
		`UPDATE organizations SET custom_system_prompt=? WHERE id=?`, nullString(prompt), orgID)
	return err
}

// UpdateOrganizationTimezone sets the timezone column.
func (d *DB) UpdateOrganizationTimezone(orgID int64, tz string) error {
	_, err := d.pool.Exec(`UPDATE organizations SET timezone=? WHERE id=?`, tz, orgID)
	return err
}

// GetOrganizationVoiceSettings returns TTS settings for an org with fallback
// defaults. When the org has nothing configured (or the row is missing), we
// return Sarvam / Aditya / English — the platform-wide default the user asked
// to keep for all calls. Each column also coalesces individually so partial
// configs don't leak blanks into the call pipeline.
func (d *DB) GetOrganizationVoiceSettings(orgID int64) (VoiceSettings, error) {
	var provider, voiceID, lang sql.NullString
	var maxDuration sql.NullInt64
	err := d.pool.QueryRow(
		`SELECT tts_provider, tts_voice_id, tts_language, max_call_duration_seconds FROM organizations WHERE id=?`, orgID,
	).Scan(&provider, &voiceID, &lang, &maxDuration)
	if errors.Is(err, sql.ErrNoRows) || err != nil {
		return VoiceSettings{
			TTSProvider:            DefaultTTSProvider,
			TTSVoiceID:             DefaultVoiceIDFor(DefaultTTSProvider),
			TTSLanguage:            DefaultTTSLanguage,
			MaxCallDurationSeconds: 0,
		}, nil
	}
	prov := coalesceStr(provider.String, DefaultTTSProvider)
	return VoiceSettings{
		TTSProvider:            prov,
		TTSVoiceID:             coalesceStr(voiceID.String, DefaultVoiceIDFor(prov)),
		TTSLanguage:            coalesceStr(lang.String, DefaultTTSLanguage),
		MaxCallDurationSeconds: clampCallDurationSeconds(int(maxDuration.Int64)),
	}, nil
}

// Platform-wide voice defaults used whenever an org or campaign has nothing
// configured. Kept as package-level constants so every fallback path (org,
// campaign, pipeline) stays in sync.
//
// DefaultTTSVoiceID is the "Aditya persona" equivalent on Sarvam. Other
// providers don't have a voice literally named Aditya, so DefaultVoiceIDFor
// returns the closest-role male default for each provider.
const (
	DefaultTTSProvider = "sarvam"
	DefaultTTSVoiceID  = "aditya"
	DefaultTTSLanguage = "en"
)

// DefaultVoiceIDFor returns the platform-default voice ID for a given TTS
// provider — the "Aditya persona" equivalent that the caller expects when no
// voice is explicitly configured. Voice IDs are provider-specific, so we keep
// one canonical mapping in one place.
func DefaultVoiceIDFor(provider string) string {
	switch provider {
	case "smallest", "smallestai":
		return "raj" // first male voice, analogous role to Aditya
	case "elevenlabs":
		return "s0oIsoSJ9raiUm7DJNzW" // voices.js marks this as "⭐ Default Voice"
	case "sarvam", "":
		return DefaultTTSVoiceID
	}
	return DefaultTTSVoiceID
}

// SaveOrganizationVoiceSettings updates tts_* columns on an org.
func (d *DB) SaveOrganizationVoiceSettings(orgID int64, vs VoiceSettings) error {
	_, err := d.pool.Exec(
		`UPDATE organizations SET tts_provider=?, tts_voice_id=?, tts_language=?, max_call_duration_seconds=? WHERE id=?`,
		nullString(vs.TTSProvider), nullString(vs.TTSVoiceID), nullString(vs.TTSLanguage),
		nullInt(vs.MaxCallDurationSeconds), orgID)
	return err
}

func nullInt(v int) any {
	if v <= 0 {
		return nil
	}
	return v
}

func clampCallDurationSeconds(v int) int {
	if v <= 0 {
		return 0
	}
	if v < 30 {
		return 30
	}
	if v > 60*60 {
		return 60 * 60
	}
	return v
}

// EnsureProductsTable creates the products table if it doesn't exist and adds
// any columns that may be missing on legacy schemas.
func (d *DB) EnsureProductsTable() error {
	_, err := d.pool.Exec(`
		CREATE TABLE IF NOT EXISTS products (
			id BIGINT AUTO_INCREMENT PRIMARY KEY,
			org_id BIGINT NOT NULL,
			name VARCHAR(255) NOT NULL,
			website_url TEXT,
			scraped_info LONGTEXT,
			manual_notes LONGTEXT,
			agent_persona LONGTEXT,
			call_flow_instructions LONGTEXT,
			image_urls LONGTEXT,
			manual_images LONGTEXT,
			created_at TIMESTAMP NULL DEFAULT CURRENT_TIMESTAMP,
			INDEX idx_org_id (org_id),
			FOREIGN KEY (org_id) REFERENCES organizations(id) ON DELETE CASCADE
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
	`)
	if err != nil {
		return err
	}
	// Add columns that may be missing on older schemas. Run each ALTER
	// separately and ignore "Duplicate column name" errors so this stays safe
	// on MySQL/MariaDB versions that don't support ADD COLUMN IF NOT EXISTS.
	columns := []struct{ name, def string }{
		{"agent_persona", "LONGTEXT"},
		{"call_flow_instructions", "LONGTEXT"},
		{"image_urls", "LONGTEXT"},
		{"manual_images", "LONGTEXT"},
	}
	for _, col := range columns {
		_, alterErr := d.pool.Exec(fmt.Sprintf("ALTER TABLE products ADD COLUMN %s %s", col.name, col.def))
		if alterErr != nil && !strings.Contains(alterErr.Error(), "Duplicate column name") {
			return alterErr
		}
	}
	return nil
}

// ProductImage holds a manually uploaded image with a human-readable label.
type ProductImage struct {
	URL   string `json:"url"`
	Label string `json:"label"`
}

// Product mirrors the products table.
type Product struct {
	ID                   int64          `json:"id"`
	OrgID                int64          `json:"org_id"`
	Name                 string         `json:"name"`
	WebsiteURL           string         `json:"website_url"`
	ScrapedInfo          string         `json:"scraped_info"`
	ManualNotes          string         `json:"manual_notes"`
	AgentPersona         string         `json:"agent_persona"`
	CallFlowInstructions string         `json:"call_flow_instructions"`
	CreatedAt            string         `json:"created_at"`
	ImageURLs            []string       `json:"image_urls"`    // scraped from website
	ManualImages         []ProductImage `json:"manual_images"` // manually uploaded via UI
}

const productCols = `id, org_id, name,
	COALESCE(website_url,''), COALESCE(scraped_info,''), COALESCE(manual_notes,''),
	COALESCE(agent_persona,''), COALESCE(call_flow_instructions,''),
	DATE_FORMAT(created_at,'%Y-%m-%d %H:%i:%s'),
	COALESCE(image_urls,'[]'), COALESCE(manual_images,'[]')`

func scanProduct(row interface{ Scan(...any) error }) (*Product, error) {
	p := &Product{}
	var imageURLsJSON, manualImagesJSON string
	err := row.Scan(&p.ID, &p.OrgID, &p.Name,
		&p.WebsiteURL, &p.ScrapedInfo, &p.ManualNotes,
		&p.AgentPersona, &p.CallFlowInstructions, &p.CreatedAt,
		&imageURLsJSON, &manualImagesJSON)
	if err != nil {
		return p, err
	}
	if imageURLsJSON != "" && imageURLsJSON != "[]" {
		_ = json.Unmarshal([]byte(imageURLsJSON), &p.ImageURLs)
	}
	if p.ImageURLs == nil {
		p.ImageURLs = []string{}
	}
	if manualImagesJSON != "" && manualImagesJSON != "[]" {
		_ = json.Unmarshal([]byte(manualImagesJSON), &p.ManualImages)
	}
	if p.ManualImages == nil {
		p.ManualImages = []ProductImage{}
	}
	return p, nil
}

// GetProductsByOrg returns one row per unique (org_id, lower(name)) — when
// duplicate names exist (legacy data from before the createProduct dup-check
// landed), the LOWEST id wins. The campaign Product dropdown was rendering
// indistinguishable rows ("EmpMonitor" with two ids) and the user couldn't
// tell which one to pick. Issue #71 — frontend already deduped on render,
// but doing it server-side is the right place so every consumer benefits.
func (d *DB) GetProductsByOrg(orgID int64) ([]Product, error) {
	rows, err := d.pool.Query(
		`SELECT `+productCols+` FROM products
		 WHERE org_id=?
		   AND id IN (
		     SELECT MIN(id) FROM products
		     WHERE org_id=?
		     GROUP BY LOWER(TRIM(name))
		   )
		 ORDER BY id DESC`, orgID, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []Product
	for rows.Next() {
		p, err := scanProduct(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, *p)
	}
	return list, rows.Err()
}

// GetProductByID fetches one product. Returns nil when not found.
func (d *DB) GetProductByID(id int64) (*Product, error) {
	row := d.pool.QueryRow(`SELECT `+productCols+` FROM products WHERE id=?`, id)
	p, err := scanProduct(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return p, err
}

// GetProductByOrgAndName fetches a product by org+name (case-insensitive,
// trimmed). Returns nil when not found. Used by createProduct to prevent
// duplicate (org_id, name) rows that today render as "EmpMonitor / EmpMonitor"
// in the campaign product dropdown.
func (d *DB) GetProductByOrgAndName(orgID int64, name string) (*Product, error) {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return nil, nil
	}
	row := d.pool.QueryRow(
		`SELECT `+productCols+` FROM products
		 WHERE org_id=? AND LOWER(name)=LOWER(?)
		 ORDER BY id ASC LIMIT 1`, orgID, trimmed)
	p, err := scanProduct(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return p, err
}

// CreateProduct inserts a new product. Returns the new ID.
func (d *DB) CreateProduct(orgID int64, name, websiteURL, manualNotes string) (int64, error) {
	res, err := d.pool.Exec(
		`INSERT INTO products (org_id, name, website_url, manual_notes) VALUES (?,?,?,?)`,
		orgID, name, nullString(websiteURL), nullString(manualNotes))
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// UpdateProduct updates mutable product fields. Pass empty to skip a field.
func (d *DB) UpdateProduct(id int64, name, websiteURL, scrapedInfo, manualNotes string) error {
	var parts []string
	var args []any
	if name != "" {
		parts = append(parts, "name=?")
		args = append(args, name)
	}
	if websiteURL != "" {
		parts = append(parts, "website_url=?")
		args = append(args, websiteURL)
	}
	if scrapedInfo != "" {
		parts = append(parts, "scraped_info=?")
		args = append(args, scrapedInfo)
	}
	if manualNotes != "" {
		parts = append(parts, "manual_notes=?")
		args = append(args, manualNotes)
	}
	if len(parts) == 0 {
		return nil
	}
	args = append(args, id)
	_, err := d.pool.Exec(`UPDATE products SET `+strings.Join(parts, ",")+` WHERE id=?`, args...)
	return err
}

// UpdateProductImageURLs updates the image_urls JSON column for a product.
// Requires: ALTER TABLE products ADD COLUMN image_urls TEXT NULL
// (run this migration on the DB before using this feature).
func (d *DB) UpdateProductImageURLs(id int64, urls []string) error {
	data, err := json.Marshal(urls)
	if err != nil {
		return err
	}
	_, err = d.pool.Exec(`UPDATE products SET image_urls=? WHERE id=?`, string(data), id)
	return err
}

// UpdateProductManualImages persists the manual_images JSON column.
func (d *DB) UpdateProductManualImages(id int64, images []ProductImage) error {
	data, err := json.Marshal(images)
	if err != nil {
		return err
	}
	_, err = d.pool.Exec(`UPDATE products SET manual_images=? WHERE id=?`, string(data), id)
	return err
}

// DeleteProduct deletes a product.
func (d *DB) DeleteProduct(id int64) error {
	_, err := d.pool.Exec(`DELETE FROM products WHERE id=?`, id)
	return err
}

// GetProductPrompt returns agent_persona and call_flow_instructions for a product.
func (d *DB) GetProductPrompt(id int64) (agentPersona, callFlow string, err error) {
	err = d.pool.QueryRow(
		`SELECT COALESCE(agent_persona,''), COALESCE(call_flow_instructions,'') FROM products WHERE id=?`, id,
	).Scan(&agentPersona, &callFlow)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", nil
	}
	return
}

// UpdateProductPrompt saves agent_persona and call_flow_instructions.
func (d *DB) UpdateProductPrompt(id int64, agentPersona, callFlow string) error {
	_, err := d.pool.Exec(
		`UPDATE products SET agent_persona=?, call_flow_instructions=? WHERE id=?`,
		nullString(agentPersona), nullString(callFlow), id)
	return err
}

// Task mirrors the tasks table joined with lead first/last name.
type Task struct {
	ID          int64  `json:"id"`
	LeadID      int64  `json:"lead_id"`
	Department  string `json:"department"`
	Description string `json:"description"`
	Status      string `json:"status"`
	FirstName   string `json:"first_name"`
	LastName    string `json:"last_name"`
}

// GetAllTasks returns all tasks for an org joined with lead name.
func (d *DB) GetAllTasks(orgID int64) ([]Task, error) {
	rows, err := d.pool.Query(`
		SELECT t.id, t.lead_id, COALESCE(t.department,''), COALESCE(t.description,''),
			COALESCE(t.status,'Pending'), l.first_name, COALESCE(l.last_name,'')
		FROM tasks t
		JOIN leads l ON t.lead_id=l.id
		WHERE l.org_id=?
		ORDER BY t.status DESC, t.id DESC`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []Task
	for rows.Next() {
		var t Task
		if err := rows.Scan(&t.ID, &t.LeadID, &t.Department, &t.Description,
			&t.Status, &t.FirstName, &t.LastName); err != nil {
			return nil, err
		}
		list = append(list, t)
	}
	return list, rows.Err()
}

// CompleteTask sets task status to 'Complete', scoped to the org via the
// task's linked lead so one tenant cannot complete another tenant's task.
func (d *DB) CompleteTask(id, orgID int64) error {
	res, err := d.pool.Exec(`
		UPDATE tasks t
		JOIN leads l ON l.id = t.lead_id
		SET t.status='Complete'
		WHERE t.id=? AND l.org_id=?`, id, orgID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// Pronunciation mirrors the pronunciation_guide table.
type Pronunciation struct {
	ID       int64  `json:"id"`
	Word     string `json:"word"`
	Phonetic string `json:"phonetic"`
}

// GetAllPronunciations returns all pronunciation entries ordered by word.
//
// Rows where word and phonetic are identical (case-insensitive, after trim)
// are filtered out at the SQL level. Such rules are no-ops by definition — the
// TTS engine produces the same audio whether the rule fires or not — but they
// pollute the admin UI ("BDRPL → BDRPL") and pad the LLM system prompt. The
// API (POST /api/pronunciation) and the React form both reject identical
// inputs, so any row still matching today is legacy data that pre-dates that
// validation. Filtering at read time means we don't need a one-shot DB
// migration to clean it up across every deployment.
func (d *DB) GetAllPronunciations() ([]Pronunciation, error) {
	rows, err := d.pool.Query(
		`SELECT id, word, phonetic FROM pronunciation_guide
		 WHERE LOWER(TRIM(word)) <> LOWER(TRIM(phonetic))
		 ORDER BY word`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []Pronunciation
	for rows.Next() {
		var p Pronunciation
		if err := rows.Scan(&p.ID, &p.Word, &p.Phonetic); err != nil {
			return nil, err
		}
		list = append(list, p)
	}
	return list, rows.Err()
}

// UpsertPronunciation inserts a new entry or updates the phonetic for an existing word.
func (d *DB) UpsertPronunciation(word, phonetic string) error {
	var id int64
	err := d.pool.QueryRow(`SELECT id FROM pronunciation_guide WHERE word=?`, word).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		_, err = d.pool.Exec(`INSERT INTO pronunciation_guide (word, phonetic) VALUES (?,?)`, word, phonetic)
		return err
	}
	if err != nil {
		return err
	}
	_, err = d.pool.Exec(`UPDATE pronunciation_guide SET phonetic=? WHERE word=?`, phonetic, word)
	return err
}

// DeletePronunciation deletes a pronunciation entry. Returns true if deleted.
func (d *DB) DeletePronunciation(id int64) (bool, error) {
	res, err := d.pool.Exec(`DELETE FROM pronunciation_guide WHERE id=?`, id)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// Report holds org-level aggregate metrics.
type Report struct {
	TotalLeads           int64 `json:"total_leads"`
	ClosedDeals          int64 `json:"closed_deals"`
	ValidSitePunches     int64 `json:"valid_site_punches"`
	PendingInternalTasks int64 `json:"pending_internal_tasks"`
}

// GetReports returns high-level metrics for an org.
func (d *DB) GetReports(orgID int64) (Report, error) {
	var r Report
	if err := d.pool.QueryRow(
		`SELECT COUNT(*) FROM leads WHERE org_id=?`, orgID,
	).Scan(&r.TotalLeads); err != nil {
		return r, err
	}
	if err := d.pool.QueryRow(
		`SELECT COUNT(*) FROM leads WHERE status='Closed' AND org_id=?`, orgID,
	).Scan(&r.ClosedDeals); err != nil {
		return r, err
	}
	if err := d.pool.QueryRow(`
		SELECT COUNT(*) FROM punches p JOIN sites s ON p.site_id=s.id
		WHERE p.status='Valid' AND s.org_id=?`, orgID,
	).Scan(&r.ValidSitePunches); err != nil {
		return r, err
	}
	err := d.pool.QueryRow(`
		SELECT COUNT(*) FROM tasks t JOIN leads l ON t.lead_id=l.id
		WHERE t.status='Pending' AND l.org_id=?`, orgID,
	).Scan(&r.PendingInternalTasks)
	return r, err
}
