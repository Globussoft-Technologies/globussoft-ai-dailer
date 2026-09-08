package db

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// EnsureOrgExotelAccountsTable creates the org_exotel_accounts table if it doesn't exist.
func (d *DB) EnsureOrgExotelAccountsTable() error {
	_, err := d.pool.Exec(`
		CREATE TABLE IF NOT EXISTS org_exotel_accounts (
			id BIGINT AUTO_INCREMENT PRIMARY KEY,
			org_id BIGINT NOT NULL,
			user_id BIGINT DEFAULT NULL,
			provider VARCHAR(50) DEFAULT 'exotel',
			name VARCHAR(255) NOT NULL,
			api_key VARCHAR(512) NOT NULL,
			api_token VARCHAR(512) NOT NULL,
			api_secret VARCHAR(512) DEFAULT '',
			account_sid VARCHAR(255) NOT NULL,
			caller_id VARCHAR(50) NOT NULL,
			app_id VARCHAR(255) DEFAULT '',
			app_type VARCHAR(20) DEFAULT 'exoml',
			direction VARCHAR(20) DEFAULT 'outbound',
			region VARCHAR(50) DEFAULT '',
			subdomain VARCHAR(255) DEFAULT '',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			INDEX idx_org_id (org_id),
			INDEX idx_user_org (user_id, org_id),
			CONSTRAINT fk_user_id FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
	`)
	if err != nil {
		return err
	}
	// Backward-compat: existing rows created before these columns were added
	// default to the legacy ExoML XML behaviour and the global Exotel cluster.
	_, _ = d.pool.Exec(`ALTER TABLE org_exotel_accounts ADD COLUMN app_type VARCHAR(20) DEFAULT 'exoml'`)
	_, _ = d.pool.Exec(`ALTER TABLE org_exotel_accounts ADD COLUMN direction VARCHAR(20) DEFAULT 'outbound'`)
	_, _ = d.pool.Exec(`ALTER TABLE org_exotel_accounts ADD COLUMN region VARCHAR(50) DEFAULT ''`)
	_, _ = d.pool.Exec(`ALTER TABLE org_exotel_accounts ADD COLUMN subdomain VARCHAR(255) DEFAULT ''`)
	// Backward-compat for per-agent accounts (user_id may be missing on older DBs).
	_, _ = d.pool.Exec(`ALTER TABLE org_exotel_accounts ADD COLUMN user_id BIGINT DEFAULT NULL`)
	_, _ = d.pool.Exec(`CREATE INDEX idx_user_org ON org_exotel_accounts(user_id, org_id)`)
	_, _ = d.pool.Exec(`ALTER TABLE org_exotel_accounts ADD CONSTRAINT fk_user_id FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE`)
	return nil
}

// EnsureUserAllowedExotelAccountsTable creates a junction table that records
// which org-level provider accounts a given user is allowed to see and use.
// Foreign keys are intentionally omitted because the live schema has
// users.id as INT and org_exotel_accounts.id as BIGINT, so no single FK
// column type can satisfy both. Application code enforces referential integrity.
func (d *DB) EnsureUserAllowedExotelAccountsTable() error {
	_, err := d.pool.Exec(`
		CREATE TABLE IF NOT EXISTS user_allowed_exotel_accounts (
			user_id BIGINT NOT NULL,
			exotel_account_id BIGINT NOT NULL,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (user_id, exotel_account_id),
			INDEX idx_exotel_account_id (exotel_account_id)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
	`)
	return err
}

// OrgExotelAccount holds a named set of provider credentials (Exotel or Twilio)
// stored at the org or user level. Multiple accounts let a single org run campaigns
// with different providers / sub-accounts, and individual agents can use their own
// credentials for calls they place personally.
type OrgExotelAccount struct {
	ID         int64  `json:"id"`
	OrgID      int64  `json:"org_id"`
	UserID     *int64 `json:"user_id,omitempty"`
	Provider   string `json:"provider"` // "exotel" or "twilio"
	Name       string `json:"name"`
	APIKey     string `json:"api_key"`    // Exotel: API Key   | Twilio: Auth Token
	APIToken   string `json:"api_token"`  // Exotel: API Token | Twilio: API Key SID (SK…)
	APISecret  string `json:"api_secret"` // Twilio only: API Secret
	AccountSID string `json:"account_sid"`
	CallerID   string `json:"caller_id"` // Exotel: Caller ID | Twilio: Phone Number
	AppID      string `json:"app_id"`    // Exotel: App ID    | Twilio: TwiML App SID
	AppType    string `json:"app_type"`  // Exotel: 'exoml' (legacy XML) or 'voicebot' (AgentStream JSON)
	Direction  string `json:"direction"` // "outbound" or "inbound"
	Region     string `json:"region"`    // Exotel region: in, us, sg, etc.
	Subdomain  string `json:"subdomain"` // Exotel account subdomain override
	CreatedAt  string `json:"created_at"`
}

func scanOrgExotelAccount(rows *sql.Rows, a *OrgExotelAccount) error {
	var userID sql.NullInt64
	err := rows.Scan(&a.ID, &a.OrgID, &userID, &a.Provider, &a.Name, &a.APIKey, &a.APIToken,
		&a.APISecret, &a.AccountSID, &a.CallerID, &a.AppID, &a.AppType, &a.Direction,
		&a.Region, &a.Subdomain, &a.CreatedAt)
	if err != nil {
		return err
	}
	if userID.Valid {
		a.UserID = &userID.Int64
	}
	return nil
}

func orgExotelAccountSelectColumns() string {
	return `id, org_id, user_id, COALESCE(provider,'exotel'), name, api_key, api_token,
		COALESCE(api_secret,''), account_sid, caller_id,
		COALESCE(app_id,''), COALESCE(app_type,'exoml'), COALESCE(direction,'outbound'),
		COALESCE(region,''), COALESCE(subdomain,''), DATE_FORMAT(created_at,'%Y-%m-%d %H:%i:%s')`
}

// GetOrgExotelAccounts returns org-level provider accounts for an org (user_id IS NULL).
func (d *DB) GetOrgExotelAccounts(orgID int64) ([]OrgExotelAccount, error) {
	rows, err := d.pool.Query(`
		SELECT `+orgExotelAccountSelectColumns()+`
		FROM org_exotel_accounts WHERE org_id=? AND user_id IS NULL ORDER BY id ASC`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []OrgExotelAccount
	for rows.Next() {
		var a OrgExotelAccount
		if err := scanOrgExotelAccount(rows, &a); err != nil {
			return nil, err
		}
		list = append(list, a)
	}
	return list, rows.Err()
}

// GetOrgExotelAccountByID fetches a single org-level account, scoping to orgID.
func (d *DB) GetOrgExotelAccountByID(id, orgID int64) (*OrgExotelAccount, error) {
	a := &OrgExotelAccount{}
	var userID sql.NullInt64
	err := d.pool.QueryRow(`
		SELECT `+orgExotelAccountSelectColumns()+`
		FROM org_exotel_accounts WHERE id=? AND org_id=? AND user_id IS NULL`, id, orgID).
		Scan(&a.ID, &a.OrgID, &userID, &a.Provider, &a.Name, &a.APIKey, &a.APIToken,
			&a.APISecret, &a.AccountSID, &a.CallerID, &a.AppID, &a.AppType, &a.Direction,
			&a.Region, &a.Subdomain, &a.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if userID.Valid {
		a.UserID = &userID.Int64
	}
	return a, nil
}

// CreateOrgExotelAccount inserts a new org-level account and returns its ID.
func (d *DB) CreateOrgExotelAccount(orgID int64, provider, name, apiKey, apiToken, apiSecret, accountSID, callerID, appID, appType, direction, region, subdomain string) (int64, error) {
	if appType == "" {
		appType = "exoml"
	}
	if direction == "" {
		direction = "outbound"
	}
	res, err := d.pool.Exec(`
		INSERT INTO org_exotel_accounts (org_id, user_id, provider, name, api_key, api_token, api_secret, account_sid, caller_id, app_id, app_type, direction, region, subdomain)
		VALUES (?,NULL,?,?,?,?,?,?,?,?,?,?,?,?)`,
		orgID, provider, name, apiKey, apiToken, apiSecret, accountSID, callerID, appID, appType, direction, region, subdomain)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// UpdateOrgExotelAccount updates all mutable fields on an existing org-level account.
func (d *DB) UpdateOrgExotelAccount(id, orgID int64, provider, name, apiKey, apiToken, apiSecret, accountSID, callerID, appID, appType, direction, region, subdomain string) error {
	if appType == "" {
		appType = "exoml"
	}
	if direction == "" {
		direction = "outbound"
	}
	_, err := d.pool.Exec(`
		UPDATE org_exotel_accounts
		SET provider=?, name=?, api_key=?, api_token=?, api_secret=?, account_sid=?, caller_id=?, app_id=?, app_type=?, direction=?, region=?, subdomain=?
		WHERE id=? AND org_id=? AND user_id IS NULL`,
		provider, name, apiKey, apiToken, apiSecret, accountSID, callerID, appID, appType, direction, region, subdomain, id, orgID)
	return err
}

// DeleteOrgExotelAccount removes an org-level account, scoping the delete to orgID.
func (d *DB) DeleteOrgExotelAccount(id, orgID int64) error {
	_, err := d.pool.Exec(`DELETE FROM org_exotel_accounts WHERE id=? AND org_id=? AND user_id IS NULL`, id, orgID)
	return err
}

// GetUserExotelAccounts returns provider accounts owned by a specific user within an org.
func (d *DB) GetUserExotelAccounts(userID, orgID int64) ([]OrgExotelAccount, error) {
	rows, err := d.pool.Query(`
		SELECT `+orgExotelAccountSelectColumns()+`
		FROM org_exotel_accounts WHERE user_id=? AND org_id=? ORDER BY id ASC`, userID, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []OrgExotelAccount
	for rows.Next() {
		var a OrgExotelAccount
		if err := scanOrgExotelAccount(rows, &a); err != nil {
			return nil, err
		}
		list = append(list, a)
	}
	return list, rows.Err()
}

// GetUserExotelAccountByID fetches a single user-owned account, scoping to userID and orgID.
func (d *DB) GetUserExotelAccountByID(id, userID, orgID int64) (*OrgExotelAccount, error) {
	a := &OrgExotelAccount{}
	var uid sql.NullInt64
	err := d.pool.QueryRow(`
		SELECT `+orgExotelAccountSelectColumns()+`
		FROM org_exotel_accounts WHERE id=? AND user_id=? AND org_id=?`, id, userID, orgID).
		Scan(&a.ID, &a.OrgID, &uid, &a.Provider, &a.Name, &a.APIKey, &a.APIToken,
			&a.APISecret, &a.AccountSID, &a.CallerID, &a.AppID, &a.AppType,
			&a.Direction, &a.Region, &a.Subdomain, &a.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if uid.Valid {
		a.UserID = &uid.Int64
	}
	return a, nil
}

// CreateUserExotelAccount inserts a user-owned provider account and returns its ID.
func (d *DB) CreateUserExotelAccount(userID, orgID int64, provider, name, apiKey, apiToken, apiSecret, accountSID, callerID, appID, appType, direction, region, subdomain string) (int64, error) {
	if appType == "" {
		appType = "exoml"
	}
	if direction == "" {
		direction = "outbound"
	}
	res, err := d.pool.Exec(`
		INSERT INTO org_exotel_accounts (org_id, user_id, provider, name, api_key, api_token, api_secret, account_sid, caller_id, app_id, app_type, direction, region, subdomain)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		orgID, userID, provider, name, apiKey, apiToken, apiSecret, accountSID, callerID, appID, appType, direction, region, subdomain)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// UpdateUserExotelAccount updates all mutable fields on a user-owned account.
func (d *DB) UpdateUserExotelAccount(id, userID, orgID int64, provider, name, apiKey, apiToken, apiSecret, accountSID, callerID, appID, appType, direction, region, subdomain string) error {
	if appType == "" {
		appType = "exoml"
	}
	if direction == "" {
		direction = "outbound"
	}
	_, err := d.pool.Exec(`
		UPDATE org_exotel_accounts
		SET provider=?, name=?, api_key=?, api_token=?, api_secret=?, account_sid=?, caller_id=?, app_id=?, app_type=?, direction=?, region=?, subdomain=?
		WHERE id=? AND user_id=? AND org_id=?`,
		provider, name, apiKey, apiToken, apiSecret, accountSID, callerID, appID, appType, direction, region, subdomain, id, userID, orgID)
	return err
}

// DeleteUserExotelAccount removes a user-owned account.
func (d *DB) DeleteUserExotelAccount(id, userID, orgID int64) error {
	_, err := d.pool.Exec(`DELETE FROM org_exotel_accounts WHERE id=? AND user_id=? AND org_id=?`, id, userID, orgID)
	return err
}

// GetExotelAccountByIDInOrg fetches any account (org or user-owned) by id and org_id.
// Used when an account ID is supplied by the client and we need to verify it belongs to the org.
func (d *DB) GetExotelAccountByIDInOrg(id, orgID int64) (*OrgExotelAccount, error) {
	a := &OrgExotelAccount{}
	var userID sql.NullInt64
	err := d.pool.QueryRow(`
		SELECT `+orgExotelAccountSelectColumns()+`
		FROM org_exotel_accounts WHERE id=? AND org_id=?`, id, orgID).
		Scan(&a.ID, &a.OrgID, &userID, &a.Provider, &a.Name, &a.APIKey, &a.APIToken,
			&a.APISecret, &a.AccountSID, &a.CallerID, &a.AppID, &a.AppType,
			&a.Direction, &a.Region, &a.Subdomain, &a.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if userID.Valid {
		a.UserID = &userID.Int64
	}
	return a, nil
}

// GetCampaignExotelAccountID returns the exotel_account_id linked to a campaign (0 if none).
func (d *DB) GetCampaignExotelAccountID(campaignID int64) (int64, error) {
	var id int64
	err := d.pool.QueryRow(`SELECT COALESCE(exotel_account_id,0) FROM campaigns WHERE id=?`, campaignID).Scan(&id)
	return id, err
}

// GetExotelAppTypeByAppID returns the app_type for the first org_exotel_accounts
// row matching the given app_id/flow_id. Empty string means no match.
func (d *DB) GetExotelAppTypeByAppID(appID string) string {
	if appID == "" {
		return ""
	}
	var appType string
	_ = d.pool.QueryRow(`SELECT COALESCE(app_type,'exoml') FROM org_exotel_accounts WHERE app_id=? LIMIT 1`, appID).Scan(&appType)
	return appType
}

// GetInboundTataAccountByDID returns the inbound Tata account that owns a DID.
func (d *DB) GetInboundTataAccountByDID(did string) (*OrgExotelAccount, error) {
	a := &OrgExotelAccount{}
	err := d.pool.QueryRow(`
		SELECT id, org_id, COALESCE(provider,'tata'), name, api_key, api_token,
		       COALESCE(api_secret,''), account_sid, caller_id,
		       COALESCE(app_id,''), COALESCE(app_type,'exoml'), COALESCE(direction,'inbound'),
		       COALESCE(region,''), COALESCE(subdomain,''), DATE_FORMAT(created_at,'%Y-%m-%d %H:%i:%s')
		FROM org_exotel_accounts
		WHERE provider IN ('tata','smartflo','tata_tele')
		  AND COALESCE(direction,'outbound')='inbound'
		  AND REPLACE(REPLACE(caller_id,'+',''),' ','')=REPLACE(REPLACE(?,'+',''),' ','')
		ORDER BY id DESC
		LIMIT 1`, did).
		Scan(&a.ID, &a.OrgID, &a.Provider, &a.Name, &a.APIKey, &a.APIToken,
			&a.APISecret, &a.AccountSID, &a.CallerID, &a.AppID, &a.AppType, &a.Direction,
			&a.Region, &a.Subdomain, &a.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return a, err
}

// GetOrgExotelAccountCreds returns an ExotelCreds from an org-level account.
// Returns zero-value ExotelCreds (IsSet()=false) when the ID is 0 or not found.
func (d *DB) GetOrgExotelAccountCreds(accountID, orgID int64) (ExotelCreds, error) {
	if accountID == 0 {
		return ExotelCreds{}, nil
	}
	a, err := d.GetOrgExotelAccountByID(accountID, orgID)
	if err != nil || a == nil {
		return ExotelCreds{}, err
	}
	return ExotelCreds{
		AccountID:  a.ID,
		Provider:   a.Provider,
		APIKey:     a.APIKey,
		APIToken:   a.APIToken,
		APISecret:  a.APISecret,
		AccountSID: a.AccountSID,
		CallerID:   a.CallerID,
		AppID:      a.AppID,
		AppType:    a.AppType,
		Direction:  a.Direction,
		Region:     a.Region,
		Subdomain:  a.Subdomain,
	}, nil
}

// GetUserExotelAccountCreds returns the first active provider account owned by a user
// in an org as ExotelCreds. Returns zero-value ExotelCreds when the user has no personal account.
func (d *DB) GetUserExotelAccountCreds(userID, orgID int64) (ExotelCreds, error) {
	if userID == 0 || orgID == 0 {
		return ExotelCreds{}, nil
	}
	accounts, err := d.GetUserExotelAccounts(userID, orgID)
	if err != nil {
		return ExotelCreds{}, err
	}
	if len(accounts) == 0 {
		return ExotelCreds{}, nil
	}
	a := accounts[0]
	return ExotelCreds{
		AccountID:  a.ID,
		Provider:   a.Provider,
		APIKey:     a.APIKey,
		APIToken:   a.APIToken,
		APISecret:  a.APISecret,
		AccountSID: a.AccountSID,
		CallerID:   a.CallerID,
		AppID:      a.AppID,
		AppType:    a.AppType,
		Direction:  a.Direction,
		Region:     a.Region,
		Subdomain:  a.Subdomain,
	}, nil
}

// accountToCreds converts an OrgExotelAccount to ExotelCreds.
func accountToCreds(a *OrgExotelAccount) ExotelCreds {
	if a == nil {
		return ExotelCreds{}
	}
	return ExotelCreds{
		AccountID:  a.ID,
		Provider:   a.Provider,
		APIKey:     a.APIKey,
		APIToken:   a.APIToken,
		APISecret:  a.APISecret,
		AccountSID: a.AccountSID,
		CallerID:   a.CallerID,
		AppID:      a.AppID,
		AppType:    a.AppType,
		Direction:  a.Direction,
		Region:     a.Region,
		Subdomain:  a.Subdomain,
	}
}

// GetOrgOrUserExotelAccountCreds returns an ExotelCreds from an account by ID,
// verifying it belongs to the org and optionally to the requesting user.
// accountID 0 returns zero-value ExotelCreds. If userID is provided (>0), the account
// must be either org-level or owned by that user.
func (d *DB) GetOrgOrUserExotelAccountCreds(accountID, orgID, userID int64) (ExotelCreds, error) {
	if accountID == 0 {
		return ExotelCreds{}, nil
	}
	a, err := d.GetExotelAccountByIDInOrg(accountID, orgID)
	if err != nil || a == nil {
		return ExotelCreds{}, err
	}
	if userID > 0 && a.UserID != nil && *a.UserID != userID {
		return ExotelCreds{}, nil
	}
	return accountToCreds(a), nil
}

// GetUserAllowedExotelAccountIDs returns the IDs of org-level provider accounts
// the user is allowed to use. An empty slice means no accounts are explicitly allowed.
func (d *DB) GetUserAllowedExotelAccountIDs(userID int64) ([]int64, error) {
	rows, err := d.pool.Query(`
		SELECT exotel_account_id FROM user_allowed_exotel_accounts WHERE user_id=? ORDER BY exotel_account_id ASC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// SetUserAllowedExotelAccountIDs replaces the allowed account list for a user.
func (d *DB) SetUserAllowedExotelAccountIDs(userID, orgID int64, accountIDs []int64) error {
	tx, err := d.pool.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Remove existing allowed entries.
	if _, err := tx.Exec(`DELETE FROM user_allowed_exotel_accounts WHERE user_id=?`, userID); err != nil {
		return err
	}

	if len(accountIDs) == 0 {
		return tx.Commit()
	}

	// Verify the requested IDs belong to the same org.
	placeholders := make([]string, len(accountIDs))
	args := make([]any, 0, len(accountIDs)+1)
	for i, id := range accountIDs {
		placeholders[i] = "?"
		args = append(args, id)
	}
	args = append(args, orgID)
	var validCount int64
	countQuery := fmt.Sprintf(`
		SELECT COUNT(*) FROM org_exotel_accounts
		WHERE id IN (%s) AND org_id=? AND user_id IS NULL`, strings.Join(placeholders, ","))
	if err := tx.QueryRow(countQuery, args...).Scan(&validCount); err != nil {
		return err
	}
	if int(validCount) != len(accountIDs) {
		return errors.New("one or more accounts do not belong to this org")
	}

	// Insert new allowed entries.
	for _, id := range accountIDs {
		if _, err := tx.Exec(`
			INSERT INTO user_allowed_exotel_accounts (user_id, exotel_account_id) VALUES (?,?)`,
			userID, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}
