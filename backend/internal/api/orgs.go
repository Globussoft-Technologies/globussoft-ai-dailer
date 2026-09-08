package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/globussoft/callified-backend/internal/db"
)

// authorizeOrg returns true if the caller is allowed to access the given org.
// Super-admins may access any org; regular users may only access their own.
func (s *Server) authorizeOrg(w http.ResponseWriter, r *http.Request, orgID int64) bool {
	ac := getAuth(r)
	if ac.OrgID == orgID {
		return true
	}
	if s.isSuperAdmin(ac.Email) {
		return true
	}
	writeError(w, http.StatusForbidden, "forbidden")
	return false
}

// authorizeProduct returns true if the product belongs to the caller's org
// (or the caller is a super-admin). Writes the response and returns false on
// denial or error.
func (s *Server) authorizeProduct(w http.ResponseWriter, r *http.Request, productID int64) (*db.Product, bool) {
	ac := getAuth(r)
	if s.isSuperAdmin(ac.Email) {
		product, err := s.db.GetProductByID(productID)
		if err != nil {
			s.logger.Sugar().Errorw("authorizeProduct", "err", err)
			writeError(w, http.StatusInternalServerError, "internal error")
			return nil, false
		}
		if product == nil {
			writeError(w, http.StatusNotFound, "product not found")
			return nil, false
		}
		return product, true
	}
	product, err := s.db.GetProductByID(productID)
	if err != nil {
		s.logger.Sugar().Errorw("authorizeProduct", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return nil, false
	}
	if product == nil {
		writeError(w, http.StatusNotFound, "product not found")
		return nil, false
	}
	if product.OrgID != ac.OrgID {
		writeError(w, http.StatusForbidden, "forbidden")
		return nil, false
	}
	return product, true
}

// ── GET /api/organizations ───────────────────────────────────────────────────

// @Summary     List organizations
// @Description Returns the caller's org (or all orgs for superadmin).
// @Tags        organizations
// @Produce     json
// @Security    BearerAuth
// @Success     200  {array}   db.Organization
// @Failure     401  {object}  ErrorResponse
// @Failure     500  {object}  ErrorResponse
// @Router      /api/organizations [get]
func (s *Server) listOrgs(w http.ResponseWriter, r *http.Request) {
	ac := getAuth(r)
	// Return only the user's own org unless they're a superadmin (org_id==0).
	if ac.OrgID > 0 {
		org, err := s.db.GetOrganizationByID(ac.OrgID)
		if err != nil {
			s.logger.Sugar().Errorw("listOrgs", "err", err)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		if org == nil {
			writeJSON(w, http.StatusOK, []any{})
			return
		}
		writeJSON(w, http.StatusOK, []any{org})
		return
	}
	orgs, err := s.db.GetAllOrganizations()
	if err != nil {
		s.logger.Sugar().Errorw("listOrgs", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, emptyJSON(orgs))
}

// ── POST /api/organizations ──────────────────────────────────────────────────

// @Summary     Create organization
// @Description Creates a new organisation. Requires Admin role.
// @Tags        organizations
// @Accept      json
// @Produce     json
// @Security    BearerAuth
// @Param       body  body      object{name=string}  true  "Org name"
// @Success     201   {object}  IDResponse
// @Failure     400   {object}  ErrorResponse
// @Failure     401   {object}  ErrorResponse
// @Failure     403   {object}  ErrorResponse
// @Failure     500   {object}  ErrorResponse
// @Router      /api/organizations [post]
func (s *Server) createOrg(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Name == "" {
		writeError(w, http.StatusBadRequest, "name required")
		return
	}
	id, err := s.db.CreateOrganization(body.Name)
	if err != nil {
		s.logger.Sugar().Errorw("createOrg", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]int64{"id": id})
}

// ── DELETE /api/organizations/{id} ───────────────────────────────────────────

// @Summary     Delete organization
// @Description Permanently deletes an org. Requires Admin role.
// @Tags        organizations
// @Produce     json
// @Security    BearerAuth
// @Param       id  path      int64  true  "Org ID"
// @Success     200  {object}  DeletedResponse
// @Failure     400  {object}  ErrorResponse
// @Failure     401  {object}  ErrorResponse
// @Failure     403  {object}  ErrorResponse
// @Failure     500  {object}  ErrorResponse
// @Router      /api/organizations/{id} [delete]
func (s *Server) deleteOrg(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if !s.authorizeOrg(w, r, id) {
		return
	}
	if err := s.db.DeleteOrganization(id); err != nil {
		s.logger.Sugar().Errorw("deleteOrg", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"deleted": true})
}

// ── PUT /api/organizations/{id}/timezone ─────────────────────────────────────

// @Summary     Update org timezone
// @Description Sets the IANA timezone for the org (used for call scheduling). Requires Admin role.
// @Tags        organizations
// @Accept      json
// @Produce     json
// @Security    BearerAuth
// @Param       id    path  int64                       true  "Org ID"
// @Param       body  body  object{timezone=string}     true  "IANA timezone string (e.g. Asia/Kolkata)"
// @Success     200  {object}  BoolResponse
// @Failure     400  {object}  ErrorResponse
// @Failure     401  {object}  ErrorResponse
// @Failure     403  {object}  ErrorResponse
// @Failure     500  {object}  ErrorResponse
// @Router      /api/organizations/{id}/timezone [put]
func (s *Server) updateOrgTimezone(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if !s.authorizeOrg(w, r, id) {
		return
	}
	var body struct {
		Timezone string `json:"timezone"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Timezone == "" {
		writeError(w, http.StatusBadRequest, "timezone required")
		return
	}
	if err := s.db.UpdateOrganizationTimezone(id, body.Timezone); err != nil {
		s.logger.Sugar().Errorw("updateOrgTimezone", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"updated": true})
}

// ── GET /api/organizations/{id}/voice-settings ───────────────────────────────

// @Summary     Get org voice settings
// @Description Returns TTS provider, voice ID and language for the org.
// @Tags        organizations
// @Produce     json
// @Security    BearerAuth
// @Param       id  path      int64  true  "Org ID"
// @Success     200  {object}  db.VoiceSettings
// @Failure     400  {object}  ErrorResponse
// @Failure     401  {object}  ErrorResponse
// @Failure     500  {object}  ErrorResponse
// @Router      /api/organizations/{id}/voice-settings [get]
func (s *Server) getOrgVoiceSettings(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if !s.authorizeOrg(w, r, id) {
		return
	}
	vs, err := s.db.GetOrganizationVoiceSettings(id)
	if err != nil {
		s.logger.Sugar().Errorw("getOrgVoiceSettings", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, vs)
}

// ── PUT /api/organizations/{id}/voice-settings ───────────────────────────────

// @Summary     Save org voice settings
// @Description Updates default TTS provider, voice ID and language. Requires Admin role.
// @Tags        organizations
// @Accept      json
// @Produce     json
// @Security    BearerAuth
// @Param       id    path  int64             true  "Org ID"
// @Param       body  body  db.VoiceSettings  true  "Voice settings"
// @Success     200  {object}  BoolResponse
// @Failure     400  {object}  ErrorResponse
// @Failure     401  {object}  ErrorResponse
// @Failure     403  {object}  ErrorResponse
// @Failure     500  {object}  ErrorResponse
// @Router      /api/organizations/{id}/voice-settings [put]
func (s *Server) saveOrgVoiceSettings(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if !s.authorizeOrg(w, r, id) {
		return
	}
	var vs db.VoiceSettings
	if err := json.NewDecoder(r.Body).Decode(&vs); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if err := s.db.SaveOrganizationVoiceSettings(id, vs); err != nil {
		s.logger.Sugar().Errorw("saveOrgVoiceSettings", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"saved": true})
}

// ── GET /api/organizations/{id}/system-prompt ────────────────────────────────
//
// Response shape matches the Python backend the frontend was written for:
//   { "auto_generated": "...", "custom_prompt": "..." }
// auto_generated is the product-knowledge context assembled from the org's
// products; custom_prompt is the optional override stored on the organization.

// @Summary     Get org system prompt
// @Description Returns the auto-generated product context and the custom override prompt for the org.
// @Tags        organizations
// @Produce     json
// @Security    BearerAuth
// @Param       id  path      int64  true  "Org ID"
// @Success     200  {object}  object{auto_generated=string,custom_prompt=string}
// @Failure     400  {object}  ErrorResponse
// @Failure     401  {object}  ErrorResponse
// @Failure     500  {object}  ErrorResponse
// @Router      /api/organizations/{id}/system-prompt [get]
func (s *Server) getOrgSystemPrompt(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if !s.authorizeOrg(w, r, id) {
		return
	}
	custom, err := s.db.GetOrgSystemPrompt(id)
	if err != nil {
		s.logger.Sugar().Errorw("getOrgSystemPrompt", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	auto := s.buildProductKnowledgeContext(id)
	writeJSON(w, http.StatusOK, map[string]string{
		"auto_generated": auto,
		"custom_prompt":  custom,
	})
}

// ── PUT /api/organizations/{id}/system-prompt ─────────────────────────────────
//
// Request body matches the Python contract: { "custom_prompt": "..." }.

// @Summary     Save org system prompt
// @Description Saves a custom LLM system prompt for the org (max 8000 chars). Requires Admin role.
// @Tags        organizations
// @Accept      json
// @Produce     json
// @Security    BearerAuth
// @Param       id    path  int64                          true  "Org ID"
// @Param       body  body  object{custom_prompt=string}  true  "Custom system prompt"
// @Success     200  {object}  BoolResponse
// @Failure     400  {object}  ErrorResponse
// @Failure     401  {object}  ErrorResponse
// @Failure     403  {object}  ErrorResponse
// @Failure     500  {object}  ErrorResponse
// @Router      /api/organizations/{id}/system-prompt [put]
func (s *Server) saveOrgSystemPrompt(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if !s.authorizeOrg(w, r, id) {
		return
	}
	var body struct {
		CustomPrompt string `json:"custom_prompt"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	// Cap the system prompt length. The textarea is unbounded in the UI and
	// the LLM's context window has a limit; longer prompts silently truncate
	// or burn tokens on every call. 8000 chars is comfortably under any
	// modern context window while leaving room for product knowledge,
	// pronunciation, and the lead context that wraps it. Issue #62.
	const maxPromptLen = 8000
	if n := len(body.CustomPrompt); n > maxPromptLen {
		writeError(w, http.StatusBadRequest,
			fmt.Sprintf("custom_prompt is %d characters; max is %d", n, maxPromptLen))
		return
	}
	if err := s.db.SaveOrgSystemPrompt(id, body.CustomPrompt); err != nil {
		s.logger.Sugar().Errorw("saveOrgSystemPrompt", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"saved": true})
}

// buildProductKnowledgeContext mirrors Python's get_product_knowledge_context:
// joins every product under the org into a single block the LLM can consume.
// Returns "" when the org has no products.
func (s *Server) buildProductKnowledgeContext(orgID int64) string {
	products, err := s.db.GetProductsByOrg(orgID)
	if err != nil || len(products) == 0 {
		return ""
	}
	orgName := ""
	if org, err := s.db.GetOrganizationByID(orgID); err == nil && org != nil {
		orgName = org.Name
	}
	parts := make([]string, 0, len(products))
	for _, p := range products {
		info := fmt.Sprintf("Product: %s (by %s)", p.Name, orgName)
		if p.ScrapedInfo != "" {
			info += " — " + p.ScrapedInfo
		}
		if p.ManualNotes != "" {
			info += " | Admin notes: " + p.ManualNotes
		}
		parts = append(parts, info)
	}
	return "\n\n[PRODUCT KNOWLEDGE - Yeh information use karo jab user product ke baare mein puchhe]:\n" + strings.Join(parts, "\n")
}

// ── GET /api/organizations/{id}/products ─────────────────────────────────────

// @Summary     List products
// @Description Returns all products for an org.
// @Tags        products
// @Produce     json
// @Security    BearerAuth
// @Param       id  path      int64  true  "Org ID"
// @Success     200  {array}   db.Product
// @Failure     400  {object}  ErrorResponse
// @Failure     401  {object}  ErrorResponse
// @Failure     500  {object}  ErrorResponse
// @Router      /api/organizations/{id}/products [get]
func (s *Server) listProducts(w http.ResponseWriter, r *http.Request) {
	if !s.requirePermission(w, r, "products.view") {
		return
	}
	id, err := parseID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if !s.authorizeOrg(w, r, id) {
		return
	}
	products, err := s.db.GetProductsByOrg(id)
	if err != nil {
		s.logger.Sugar().Errorw("listProducts", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, emptyJSON(products))
}

// ── POST /api/organizations/{id}/products ────────────────────────────────────

type productCreateRequest struct {
	Name        string `json:"name"`
	WebsiteURL  string `json:"website_url"`
	ManualNotes string `json:"manual_notes"`
}

// @Summary     Create product
// @Description Creates a new product under an org. Requires Admin role.
// @Tags        products
// @Accept      json
// @Produce     json
// @Security    BearerAuth
// @Param       id    path  int64                 true  "Org ID"
// @Param       body  body  productCreateRequest  true  "Product data"
// @Success     201   {object}  IDResponse
// @Failure     400   {object}  ErrorResponse
// @Failure     401   {object}  ErrorResponse
// @Failure     403   {object}  ErrorResponse
// @Failure     409   {object}  ErrorResponse  "product name already exists"
// @Failure     500   {object}  ErrorResponse
// @Router      /api/organizations/{id}/products [post]
func (s *Server) createProduct(w http.ResponseWriter, r *http.Request) {
	if !s.requirePermission(w, r, "products.manage") {
		return
	}
	orgID, err := parseID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid org id")
		return
	}
	if !s.authorizeOrg(w, r, orgID) {
		return
	}
	var req productCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" {
		writeError(w, http.StatusBadRequest, "name required")
		return
	}
	// Reject duplicates within the same org. A product with the same name
	// (case-insensitive, trimmed) shows up as two indistinguishable rows in
	// the campaign dropdown — return the existing row's id so the frontend
	// can select it instead of creating a confusing duplicate.
	if existing, lookupErr := s.db.GetProductByOrgAndName(orgID, req.Name); lookupErr == nil && existing != nil {
		writeJSON(w, http.StatusConflict, map[string]any{
			"error":         "product already exists",
			"existing_id":   existing.ID,
			"existing_name": existing.Name,
		})
		return
	}
	id, err := s.db.CreateProduct(orgID, req.Name, req.WebsiteURL, req.ManualNotes)
	if err != nil {
		s.logger.Sugar().Errorw("createProduct", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]int64{"id": id})
}

// ── PUT /api/products/{id} ───────────────────────────────────────────────────

type productUpdateRequest struct {
	Name        string `json:"name"`
	WebsiteURL  string `json:"website_url"`
	ScrapedInfo string `json:"scraped_info"`
	ManualNotes string `json:"manual_notes"`
}

// @Summary     Update product
// @Description Updates product name, website URL, scraped info and notes. Requires Admin role.
// @Tags        products
// @Accept      json
// @Produce     json
// @Security    BearerAuth
// @Param       id    path  int64                 true  "Product ID"
// @Param       body  body  productUpdateRequest  true  "Updated product data"
// @Success     200  {object}  BoolResponse
// @Failure     400  {object}  ErrorResponse
// @Failure     401  {object}  ErrorResponse
// @Failure     403  {object}  ErrorResponse
// @Failure     500  {object}  ErrorResponse
// @Router      /api/products/{id} [put]
func (s *Server) updateProduct(w http.ResponseWriter, r *http.Request) {
	if !s.requirePermission(w, r, "products.manage") {
		return
	}
	id, err := parseID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if _, ok := s.authorizeProduct(w, r, id); !ok {
		return
	}
	var req productUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if err := s.db.UpdateProduct(id, req.Name, req.WebsiteURL, req.ScrapedInfo, req.ManualNotes); err != nil {
		s.logger.Sugar().Errorw("updateProduct", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"updated": true})
}

// ── DELETE /api/products/{id} ────────────────────────────────────────────────

// @Summary     Delete product
// @Description Permanently deletes a product. Requires Admin role.
// @Tags        products
// @Produce     json
// @Security    BearerAuth
// @Param       id  path      int64  true  "Product ID"
// @Success     200  {object}  DeletedResponse
// @Failure     400  {object}  ErrorResponse
// @Failure     401  {object}  ErrorResponse
// @Failure     403  {object}  ErrorResponse
// @Failure     500  {object}  ErrorResponse
// @Router      /api/products/{id} [delete]
func (s *Server) deleteProduct(w http.ResponseWriter, r *http.Request) {
	if !s.requirePermission(w, r, "products.manage") {
		return
	}
	id, err := parseID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if _, ok := s.authorizeProduct(w, r, id); !ok {
		return
	}
	if err := s.db.DeleteProduct(id); err != nil {
		s.logger.Sugar().Errorw("deleteProduct", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"deleted": true})
}

// ── GET /api/products/{id}/prompt ────────────────────────────────────────────

// @Summary     Get product prompt
// @Description Returns the agent persona and call flow instructions for a product.
// @Tags        products
// @Produce     json
// @Security    BearerAuth
// @Param       id  path      int64  true  "Product ID"
// @Success     200  {object}  object{agent_persona=string,call_flow_instructions=string}
// @Failure     400  {object}  ErrorResponse
// @Failure     401  {object}  ErrorResponse
// @Failure     500  {object}  ErrorResponse
// @Router      /api/products/{id}/prompt [get]
func (s *Server) getProductPrompt(w http.ResponseWriter, r *http.Request) {
	if !s.requirePermission(w, r, "products.view") {
		return
	}
	id, err := parseID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if _, ok := s.authorizeProduct(w, r, id); !ok {
		return
	}
	persona, callFlow, err := s.db.GetProductPrompt(id)
	if err != nil {
		s.logger.Sugar().Errorw("getProductPrompt", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"agent_persona":          persona,
		"call_flow_instructions": callFlow,
	})
}

// ── PUT /api/products/{id}/prompt ────────────────────────────────────────────

// @Summary     Update product prompt
// @Description Saves agent persona and call flow instructions for a product. Requires Admin role.
// @Tags        products
// @Accept      json
// @Produce     json
// @Security    BearerAuth
// @Param       id    path  int64                                                                   true  "Product ID"
// @Param       body  body  object{agent_persona=string,call_flow_instructions=string}              true  "Prompt fields"
// @Success     200  {object}  BoolResponse
// @Failure     400  {object}  ErrorResponse
// @Failure     401  {object}  ErrorResponse
// @Failure     403  {object}  ErrorResponse
// @Failure     500  {object}  ErrorResponse
// @Router      /api/products/{id}/prompt [put]
func (s *Server) updateProductPrompt(w http.ResponseWriter, r *http.Request) {
	if !s.requirePermission(w, r, "products.manage") {
		return
	}
	id, err := parseID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if _, ok := s.authorizeProduct(w, r, id); !ok {
		return
	}
	var body struct {
		AgentPersona         string `json:"agent_persona"`
		CallFlowInstructions string `json:"call_flow_instructions"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if err := s.db.UpdateProductPrompt(id, body.AgentPersona, body.CallFlowInstructions); err != nil {
		s.logger.Sugar().Errorw("updateProductPrompt", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"saved": true})
}

// ── Phase 4: LLM-powered generation endpoints ─────────────────────────────────

// extractImageURLs scans raw HTML for product images using multiple strategies:
// 1. og:image / twitter:image meta tags (highest quality, site-chosen hero image)
// 2. <img src>, srcset, and data-* lazy-load attributes
// 3. <source srcset> from <picture> elements
// 4. JavaScript patterns: image:"...", src:"...", photo:"...", thumbnail:"..."
// 5. CSS background-image: url(...) and data-bg attributes
// Resolves relative URLs, filters non-content images, deduplicates, returns up to 10.
func extractImageURLs(rawHTML, baseURL string) []string {
	lower := strings.ToLower(rawHTML)
	seen := map[string]bool{}     // full URL dedup
	seenName := map[string]bool{} // filename-stem dedup (prevents mobile/desktop duplicates)
	var results []string
	const maxImages = 10

	base, _ := url.Parse(baseURL)

	// Path/name segments that indicate non-product images (globally applicable).
	blacklist := []string{
		"icon", "favicon", "logo", "sprite", "1x1", ".svg",
		"placeholder", "blank", "pixel",
		"badge", "award", "certificate", "partner",
		"/mobile/", // skip mobile-specific image paths; desktop version is already collected
	}
	allowedExts := []string{".jpg", ".jpeg", ".png", ".webp", ".avif"}

	addURL := func(src string) {
		src = strings.TrimSpace(src)
		if src == "" || len(results) >= maxImages {
			return
		}
		// Strip query-string suffix that may include width hints
		if q := strings.IndexByte(src, '?'); q > 0 {
			src = src[:q]
		}
		var absURL string
		if strings.HasPrefix(src, "http://") || strings.HasPrefix(src, "https://") {
			absURL = src
		} else if base != nil {
			parsed, err := url.Parse(src)
			if err != nil {
				return
			}
			absURL = base.ResolveReference(parsed).String()
		} else {
			return
		}
		absLower := strings.ToLower(absURL)
		for _, bl := range blacklist {
			if strings.Contains(absLower, bl) {
				return
			}
		}
		hasExt := false
		for _, ext := range allowedExts {
			if strings.Contains(absLower, ext) {
				hasExt = true
				break
			}
		}
		if !hasExt {
			return
		}

		// Extract and validate filename stem for quality + dedup checks.
		stem := absURL
		if idx := strings.LastIndex(stem, "/"); idx >= 0 {
			stem = stem[idx+1:]
		}
		if dot := strings.LastIndex(stem, "."); dot >= 0 {
			stem = stem[:dot]
		}
		stemLower := strings.ToLower(stem)

		// Skip purely-numeric filenames (1.avif, 2.jpg, 001.webp — generic
		// stock images with no meaningful label).
		isNumeric := len(stemLower) > 0
		for _, c := range stemLower {
			if c < '0' || c > '9' {
				isNumeric = false
				break
			}
		}
		if isNumeric {
			return
		}
		// Skip very short stems (< 4 chars) — too ambiguous to label.
		if len(stemLower) < 4 {
			return
		}
		// Skip mobile filename variants even when not in a /mobile/ path
		// (e.g. "emp-mobile-clients.webp", "banner-mobile.avif").
		if strings.HasPrefix(stemLower, "mobile-") || strings.HasSuffix(stemLower, "-mobile") ||
			strings.Contains(stemLower, "-mobile-") || strings.Contains(stemLower, "_mobile_") {
			return
		}
		// Skip Figma/design-tool exports: Group-2343, Frame-18, Layer-5, etc.
		// These are component artifacts with no meaningful label.
		for _, prefix := range []string{"group-", "frame-", "layer-", "component-", "vector-", "ellipse-", "rectangle-"} {
			if strings.HasPrefix(stemLower, prefix) {
				rest := stemLower[len(prefix):]
				allDigits := len(rest) > 0
				for _, c := range rest {
					if c < '0' || c > '9' {
						allDigits = false
						break
					}
				}
				if allDigits {
					return // e.g. Group-2343, Frame-18
				}
			}
		}
		// Skip text-overlay images (e.g. 6-text.png, hero-text.png).
		if strings.HasSuffix(stemLower, "-text") || strings.HasSuffix(stemLower, "_text") ||
			strings.HasPrefix(stemLower, "text-") || strings.HasPrefix(stemLower, "text_") {
			return
		}
		// Skip loading animations and generic UI chrome.
		for _, generic := range []string{"loader", "loading", "spinner", "preloader", "skeleton"} {
			if stemLower == generic || strings.HasPrefix(stemLower, generic+"-") || strings.HasPrefix(stemLower, generic+"_") {
				return
			}
		}
		// Skip WordPress thumbnail crops that embed pixel dimensions (200x100,
		// 531x531, 1024x597, etc.). These are resized copies; the original
		// full-size file is the one with a meaningful name and no NxN suffix.
		for i := 1; i < len(stemLower)-2; i++ {
			if stemLower[i] == 'x' {
				// Count digits before 'x'
				j := i - 1
				for j >= 0 && stemLower[j] >= '0' && stemLower[j] <= '9' {
					j--
				}
				digitsBefore := i - j - 1
				// Count digits after 'x'
				k := i + 1
				for k < len(stemLower) && stemLower[k] >= '0' && stemLower[k] <= '9' {
					k++
				}
				digitsAfter := k - i - 1
				if digitsBefore >= 2 && digitsAfter >= 2 {
					return // e.g. image-200x100, icon531x531
				}
			}
		}

		if seen[absURL] {
			return
		}
		// Deduplicate by filename stem so we don't store both
		// /banner/morais-luxe.avif and /banner/mobile/morais-luxe.avif.
		if seenName[stemLower] {
			return
		}
		seen[absURL] = true
		seenName[stemLower] = true
		results = append(results, absURL)
	}

	// extractQuoted returns the value of a quoted attribute starting right after
	// the attribute name+= in the given string slice.
	extractQuoted := func(s string) string {
		if len(s) == 0 {
			return ""
		}
		q := s[0]
		if q != '"' && q != '\'' {
			return ""
		}
		end := strings.IndexByte(s[1:], q)
		if end < 0 {
			return ""
		}
		return s[1 : end+1]
	}

	// Strategy 1: og:image and twitter:image meta tags.
	for _, metaKey := range []string{`property="og:image"`, `name="twitter:image"`, `property="og:image:url"`} {
		idx := strings.Index(lower, metaKey)
		if idx < 0 {
			continue
		}
		contentIdx := strings.Index(lower[idx:], `content="`)
		if contentIdx < 0 {
			continue
		}
		start := idx + contentIdx + 9
		end := strings.IndexByte(lower[start:], '"')
		if end < 0 {
			continue
		}
		addURL(rawHTML[start : start+end])
	}

	// Strategy 2: <img> tag attributes — src, data-src, data-lazy-src, srcset.
	{
		pos := 0
		for len(results) < maxImages {
			idx := strings.Index(lower[pos:], "<img")
			if idx < 0 {
				break
			}
			pos += idx
			end := strings.Index(lower[pos:], ">")
			if end < 0 {
				break
			}
			tagLower := lower[pos : pos+end+1]
			tagContent := rawHTML[pos : pos+end+1]
			pos += end + 1

			for _, attr := range []string{" src=", "\tsrc=", " data-src=", " data-lazy-src=", " data-original=", " data-img="} {
				attrIdx := strings.Index(tagLower, attr)
				if attrIdx < 0 {
					continue
				}
				addURL(extractQuoted(tagContent[attrIdx+len(attr):]))
			}
			// srcset: "img1.jpg 1x, img2.jpg 2x" — take first candidate
			if si := strings.Index(tagLower, " srcset="); si >= 0 {
				val := extractQuoted(tagContent[si+8:])
				if val != "" {
					// first entry is "url descriptor" — take URL part
					first := strings.Fields(val)[0]
					addURL(first)
				}
			}
		}
	}

	// Strategy 3: <source srcset=...> inside <picture> elements.
	{
		pos := 0
		for len(results) < maxImages {
			idx := strings.Index(lower[pos:], "<source")
			if idx < 0 {
				break
			}
			pos += idx
			end := strings.Index(lower[pos:], ">")
			if end < 0 {
				break
			}
			tagLower := lower[pos : pos+end+1]
			tagContent := rawHTML[pos : pos+end+1]
			pos += end + 1
			if si := strings.Index(tagLower, " srcset="); si >= 0 {
				val := extractQuoted(tagContent[si+8:])
				if val != "" {
					first := strings.Fields(val)[0]
					addURL(first)
				}
			}
			if si := strings.Index(tagLower, " src="); si >= 0 {
				addURL(extractQuoted(tagContent[si+5:]))
			}
		}
	}

	// Strategy 4: JavaScript / JSON patterns.
	// Handles: image:"url", image: "url", img:"url", photo:"url",
	//          thumbnail:"url", src:"url", "image":"url", 'image':'url'
	jsKeywords := []string{
		`image:"`, `image: "`, `image:'`, `image: '`,
		`"image":"`, `"image": "`,
		`img:"`, `img: "`, `img:'`, `img: '`,
		`photo:"`, `photo: "`, `photo:'`, `photo: '`,
		`thumbnail:"`, `thumbnail: "`,
		`src:"`, `src: "`, `src:'`, `src: '`,
	}
	if len(results) < maxImages/2 {
		for _, kw := range jsKeywords {
			pos := 0
			for len(results) < maxImages {
				idx := strings.Index(lower[pos:], kw)
				if idx < 0 {
					break
				}
				start := pos + idx + len(kw)
				// The opening quote is the last char of kw
				q := kw[len(kw)-1]
				end := strings.IndexByte(lower[start:], q)
				if end < 0 {
					pos = start
					continue
				}
				addURL(rawHTML[start : start+end])
				pos = start + end + 1
			}
		}
	}

	// Strategy 5: CSS background-image: url(...) and data-bg attributes.
	if len(results) < maxImages {
		// data-bg="url" and data-background="url"
		for _, attr := range []string{` data-bg="`, ` data-background="`, ` data-bg='`, ` data-background='`} {
			pos := 0
			for len(results) < maxImages {
				idx := strings.Index(lower[pos:], attr)
				if idx < 0 {
					break
				}
				start := pos + idx + len(attr)
				q := attr[len(attr)-1]
				end := strings.IndexByte(lower[start:], q)
				if end < 0 {
					break
				}
				addURL(rawHTML[start : start+end])
				pos = start + end + 1
			}
		}
		// background-image: url("...") or url('...')
		pos := 0
		for len(results) < maxImages {
			idx := strings.Index(lower[pos:], "background-image:")
			if idx < 0 {
				break
			}
			pos += idx + 17
			urlIdx := strings.Index(lower[pos:], "url(")
			if urlIdx < 0 || urlIdx > 50 {
				continue
			}
			pos += urlIdx + 4
			end := strings.Index(lower[pos:], ")")
			if end < 0 {
				continue
			}
			val := strings.Trim(rawHTML[pos:pos+end], `"' `)
			addURL(val)
			pos += end + 1
		}
	}

	return results
}

// crawlSiteImages visits the homepage and all reachable internal pages (up to
// maxPages) in parallel, collects and deduplicates image URLs from every page,
// and returns up to 20 images sorted with homepage images first.
func crawlSiteImages(ctx context.Context, baseURL string, maxPages int) []string {
	rawHome, err := fetchRawHTML(ctx, baseURL)
	if err != nil {
		return nil
	}

	links := extractInternalLinks(baseURL, string(rawHome))
	if len(links) > maxPages-1 {
		links = links[:maxPages-1]
	}

	type pageResult struct {
		rawURL   string
		html     []byte
		priority int
	}

	// Fetch all sub-pages in parallel.
	subResults := make([]pageResult, len(links))
	var wg sync.WaitGroup
	for i, link := range links {
		wg.Add(1)
		go func(i int, link string) {
			defer wg.Done()
			pctx, cancel := context.WithTimeout(ctx, 10*time.Second)
			defer cancel()
			b, err := fetchRawHTML(pctx, link)
			if err == nil {
				subResults[i] = pageResult{rawURL: link, html: b, priority: pagePriority(link)}
			}
		}(i, link)
	}
	wg.Wait()

	// Collect all pages. Homepage gets priority -1 so product/feature/pricing
	// pages (priority 9-10) always contribute their images FIRST. This prevents
	// generic homepage icons from filling the cap before real app screenshots.
	pages := []pageResult{{rawURL: baseURL, html: rawHome, priority: -1}}
	for _, r := range subResults {
		if r.html != nil {
			pages = append(pages, r)
		}
	}

	// Sort highest-priority pages first (features > product > homepage).
	sort.Slice(pages, func(i, j int) bool {
		return pages[i].priority > pages[j].priority
	})

	seen := map[string]bool{}
	var all []string
	for _, p := range pages {
		for _, u := range extractImageURLs(string(p.html), p.rawURL) {
			if !seen[u] {
				seen[u] = true
				all = append(all, u)
			}
		}
	}
	if len(all) > 10 {
		all = all[:10]
	}
	return all
}

// POST /api/products/{id}/scrape
// Fetches the product's website_url and asks the LLM to extract product context.
func (s *Server) scrapeProduct(w http.ResponseWriter, r *http.Request) {
	if !s.requirePermission(w, r, "products.manage") {
		return
	}
	if s.llmProvider == nil {
		writeError(w, http.StatusServiceUnavailable, "LLM not configured")
		return
	}
	id, err := parseID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	product, ok := s.authorizeProduct(w, r, id)
	if !ok {
		return
	}
	if product.WebsiteURL == "" {
		writeError(w, http.StatusBadRequest, "product has no website_url")
		return
	}

	// Crawl homepage + up to 5 key sub-pages (60s total budget)
	crawlCtx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	allText := crawlSite(crawlCtx, product.WebsiteURL, 6)

	if len(strings.TrimSpace(allText)) < 50 {
		writeError(w, http.StatusBadGateway, "Website is not accessible or blocks automated access (e.g. Amazon, Flipkart). Please add product details manually in the notes field instead.")
		return
	}

	// Extract images from ALL crawled pages (homepage + sub-pages in parallel).
	imageURLs := crawlSiteImages(crawlCtx, product.WebsiteURL, 10)
	if len(imageURLs) > 0 {
		if err := s.db.UpdateProductImageURLs(id, imageURLs); err != nil {
			s.logger.Sugar().Warnw("scrapeProduct: UpdateProductImageURLs", "err", err, "product_id", id)
		}
	}

	// Trim to 20000 chars for LLM context
	text := allText
	if len(text) > 20000 {
		text = text[:20000]
	}

	// Ask LLM to extract detailed product info from all crawled pages
	prompt := fmt.Sprintf(`You are analyzing a company website (multiple pages) to build a detailed product brief for a sales AI agent.

Extract and summarize ALL of the following — use only information present on the pages:

1. Company name and one-line tagline
2. What the product/service does (2-3 sentences)
3. Key features and benefits (bullet points, be thorough)
4. Target customers / ideal use cases
5. Pricing, plans, or packages (exact details if visible)
6. How it works / onboarding steps
7. Unique selling points vs competitors (if mentioned)
8. Social proof: testimonials, customers, case studies, stats
9. Contact info / support channels

Be factual and detailed (max 600 words). Clearly label each section.

Website URL: %s
Crawled page content:
%s`, product.WebsiteURL, text)

	s.logger.Sugar().Infow("scrapeProduct: calling LLM", "product_id", id, "textLen", len(text))
	scraped, err := s.llmProvider.GenerateText(r.Context(), prompt, "Extract product info from this website", 3000)
	if err != nil {
		s.logger.Sugar().Errorw("scrapeProduct: LLM error", "err", err, "product_id", id)
		writeError(w, http.StatusInternalServerError, "LLM error: "+err.Error())
		return
	}
	s.logger.Sugar().Infow("scrapeProduct: success", "product_id", id, "scrapedLen", len(scraped))

	if err := s.db.UpdateProduct(id, "", "", scraped, ""); err != nil {
		s.logger.Sugar().Errorw("scrapeProduct: UpdateProduct", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"scraped_info": scraped})
}

// POST /api/products/{id}/generate-prompt
// Uses Gemini to generate agent_persona + call_flow_instructions from product info.
func (s *Server) generateProductPrompt(w http.ResponseWriter, r *http.Request) {
	if !s.requirePermission(w, r, "products.manage") {
		return
	}
	if s.llmProvider == nil {
		writeError(w, http.StatusServiceUnavailable, "LLM not configured")
		return
	}
	id, err := parseID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	product, ok := s.authorizeProduct(w, r, id)
	if !ok {
		return
	}

	context := strings.TrimSpace(product.ScrapedInfo + "\n" + product.ManualNotes)
	if context == "" {
		context = product.Name
	}

	sysprompt := `You are an expert sales trainer. Given product/service information, generate:
1. agent_persona: A short (2-3 sentence) persona description for an AI sales agent. Define tone, style, and approach.
2. call_flow_instructions: Step-by-step call flow instructions (5-7 steps). Each step on a new line, prefixed with the step number.

Return ONLY a JSON object with keys "agent_persona" and "call_flow_instructions". No markdown.`

	raw, err := s.llmProvider.GenerateText(r.Context(), sysprompt, "Product info: "+context, 1024)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "LLM error: "+err.Error())
		return
	}

	var result struct {
		AgentPersona         string `json:"agent_persona"`
		CallFlowInstructions string `json:"call_flow_instructions"`
	}
	raw = cleanJSON(raw)
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		// Fallback: return raw text
		writeJSON(w, http.StatusOK, map[string]string{"raw": raw})
		return
	}
	if err := s.db.UpdateProductPrompt(id, result.AgentPersona, result.CallFlowInstructions); err != nil {
		s.logger.Sugar().Errorw("generateProductPrompt: UpdateProductPrompt", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// POST /api/products/{id}/generate-persona
// Generates both agent_persona and call_flow_instructions from the product's
// scraped info / manual notes and persists them. Returns both fields plus a
// status flag the frontend uses to show success/error inline.
func (s *Server) generateProductPersona(w http.ResponseWriter, r *http.Request) {
	if !s.requirePermission(w, r, "products.manage") {
		return
	}
	if s.llmProvider == nil {
		writeJSON(w, http.StatusServiceUnavailable,
			map[string]string{"status": "error", "error": "LLM not configured", "message": "LLM not configured"})
		return
	}
	id, err := parseID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	product, ok := s.authorizeProduct(w, r, id)
	if !ok {
		return
	}

	context := strings.TrimSpace(product.ScrapedInfo + "\n" + product.ManualNotes)
	if context == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"status":  "error",
			"error":   "no product context",
			"message": "Please scrape the website or add manual notes first.",
		})
		return
	}

	persona, err := s.llmProvider.GenerateText(r.Context(),
		"Write a concise 2-3 sentence sales agent persona for this product. Be specific about tone and style. Return only the persona text — no preamble, no quotes.",
		"Product: "+context, 512)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{
			"status": "error", "error": err.Error(),
			"message": "LLM error: " + err.Error(),
		})
		return
	}

	callFlow, err := s.llmProvider.GenerateText(r.Context(),
		"Write a 5-7 step outbound sales call flow for this product. Use the format: \"Step 1: ...\" on each line. Be specific to the product. Return only the steps — no preamble.",
		"Product: "+context, 768)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{
			"status": "error", "error": err.Error(),
			"message": "LLM error: " + err.Error(),
		})
		return
	}

	persona = strings.TrimSpace(persona)
	callFlow = strings.TrimSpace(callFlow)

	if err := s.db.UpdateProductPrompt(id, persona, callFlow); err != nil {
		s.logger.Sugar().Errorw("generateProductPersona: UpdateProductPrompt", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"status":                 "success",
		"agent_persona":          persona,
		"call_flow_instructions": callFlow,
	})
}

// POST /api/organizations/{id}/generate-prompt
// Uses Gemini to generate a custom system prompt for the org.
func (s *Server) generateOrgPrompt(w http.ResponseWriter, r *http.Request) {
	if !s.requirePermission(w, r, "settings.manage") {
		return
	}
	if s.llmProvider == nil {
		writeError(w, http.StatusServiceUnavailable, "LLM not configured")
		return
	}
	id, err := parseID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if !s.authorizeOrg(w, r, id) {
		return
	}
	org, err := s.db.GetOrganizationByID(id)
	if err != nil || org == nil {
		writeError(w, http.StatusNotFound, "org not found")
		return
	}
	products, _ := s.db.GetProductsByOrg(id)

	var sb strings.Builder
	sb.WriteString("Company: " + org.Name + "\n")
	for _, p := range products {
		sb.WriteString("Product: " + p.Name)
		if p.ManualNotes != "" {
			sb.WriteString(" — " + p.ManualNotes)
		}
		sb.WriteString("\n")
	}

	sysprompt := `Create a detailed system prompt for an AI sales agent for this company.
The prompt should define: agent identity, communication style, goals, key product knowledge, objection handling, and how to close.
Write it in second person ("You are..."). Max 400 words.`

	generated, err := s.llmProvider.GenerateText(r.Context(), sysprompt, sb.String(), 1024)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "LLM error: "+err.Error())
		return
	}

	if err := s.db.SaveOrgSystemPrompt(id, generated); err != nil {
		s.logger.Sugar().Errorw("generateOrgPrompt: SaveOrgSystemPrompt", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"system_prompt": generated})
}

// ── helpers ───────────────────────────────────────────────────────────────────

// fetchRawHTML fetches a URL and returns raw HTML bytes (up to 300KB).
func fetchRawHTML(ctx context.Context, rawURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(io.LimitReader(resp.Body, 300_000))
}

// extractInternalLinks parses href attributes from html and returns same-host URLs.
func extractInternalLinks(baseURL, html string) []string {
	base, err := url.Parse(baseURL)
	if err != nil {
		return nil
	}
	lower := strings.ToLower(html)
	seen := map[string]bool{baseURL: true}
	var links []string
	pos := 0
	for {
		idx := strings.Index(lower[pos:], "href=")
		if idx < 0 {
			break
		}
		pos += idx + 5
		if pos >= len(lower) {
			break
		}
		quote := lower[pos]
		if quote != '"' && quote != '\'' {
			continue
		}
		pos++
		end := strings.IndexByte(lower[pos:], byte(quote))
		if end < 0 {
			break
		}
		href := html[pos : pos+end]
		pos += end + 1
		lhref := strings.ToLower(href)
		if strings.HasPrefix(lhref, "#") || strings.HasPrefix(lhref, "mailto:") ||
			strings.HasPrefix(lhref, "javascript:") || strings.HasPrefix(lhref, "tel:") {
			continue
		}
		parsed, err := url.Parse(href)
		if err != nil {
			continue
		}
		resolved := base.ResolveReference(parsed)
		resolved.Fragment = ""
		resolved.RawQuery = ""
		if resolved.Host != base.Host {
			continue
		}
		key := resolved.String()
		if seen[key] {
			continue
		}
		seen[key] = true
		links = append(links, key)
	}
	return links
}

// pagePriority scores a URL higher if it likely contains useful product info.
func pagePriority(link string) int {
	lower := strings.ToLower(link)
	priorities := []struct {
		score    int
		keywords []string
	}{
		{10, []string{"pricing", "price", "plans", "plan"}},
		{9, []string{"feature", "product", "solution", "service"}},
		{8, []string{"about", "why", "how-it-works", "how_it_works", "overview"}},
		{7, []string{"contact", "team"}},
		{6, []string{"blog", "case-study", "customer", "testimonial"}},
	}
	for _, p := range priorities {
		for _, kw := range p.keywords {
			if strings.Contains(lower, kw) {
				return p.score
			}
		}
	}
	return 0
}

// crawlSite fetches the homepage plus up to (maxPages-1) prioritized internal pages
// and returns their combined clean text, each section labelled by URL.
func crawlSite(ctx context.Context, baseURL string, maxPages int) string {
	raw, err := fetchRawHTML(ctx, baseURL)
	if err != nil {
		return ""
	}
	homeText := extractPageText(string(raw))

	links := extractInternalLinks(baseURL, string(raw))
	sort.Slice(links, func(i, j int) bool {
		return pagePriority(links[i]) > pagePriority(links[j])
	})
	if len(links) > maxPages-1 {
		links = links[:maxPages-1]
	}

	type pageResult struct {
		pageURL string
		text    string
	}
	results := make([]pageResult, len(links))
	var wg sync.WaitGroup
	for i, link := range links {
		wg.Add(1)
		go func(i int, link string) {
			defer wg.Done()
			pageCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			defer cancel()
			b, err := fetchRawHTML(pageCtx, link)
			if err == nil {
				results[i] = pageResult{pageURL: link, text: extractPageText(string(b))}
			}
		}(i, link)
	}
	wg.Wait()

	var sb strings.Builder
	sb.WriteString("=== Homepage: ")
	sb.WriteString(baseURL)
	sb.WriteString(" ===\n")
	sb.WriteString(homeText)
	for _, r := range results {
		if r.text != "" {
			sb.WriteString("\n\n=== Page: ")
			sb.WriteString(r.pageURL)
			sb.WriteString(" ===\n")
			sb.WriteString(r.text)
		}
	}
	return sb.String()
}

// extractPageText extracts human-readable text from HTML, skipping script/style/comment blocks.
// It prioritises title, meta description, and heading/paragraph content.
func extractPageText(html string) string {
	// Lower-case for tag matching (we work on a copy for matching; original for extraction)
	lower := strings.ToLower(html)
	var b strings.Builder

	// 1. Extract <title>
	if ts := extractTagContent(html, lower, "title"); ts != "" {
		b.WriteString("Title: ")
		b.WriteString(strings.TrimSpace(ts))
		b.WriteString("\n")
	}

	// 2. Extract meta description
	for _, chunk := range strings.Split(lower, "<meta") {
		if strings.Contains(chunk, "name=\"description\"") || strings.Contains(chunk, "name='description'") ||
			strings.Contains(chunk, "property=\"og:description\"") {
			// find content="..." in the original at the same offset
			idx := strings.Index(lower, "<meta"+chunk[:min(len(chunk), 300)])
			orig := html
			if idx >= 0 && idx < len(orig) {
				orig = html[idx:]
			}
			if ci := strings.Index(strings.ToLower(orig), "content="); ci >= 0 {
				rest := orig[ci+8:]
				quote := rest[0:1]
				if quote == "\"" || quote == "'" {
					end := strings.Index(rest[1:], quote)
					if end >= 0 {
						b.WriteString("Description: ")
						b.WriteString(rest[1 : end+1])
						b.WriteString("\n")
						break
					}
				}
			}
		}
	}

	// 3. Skip script/style/noscript/svg blocks, then extract readable text
	skipTags := []string{"script", "style", "noscript", "svg", "iframe", "canvas", "code", "pre"}
	pos := 0
	llen := min(len(html), len(lower))

	for pos < llen {
		// Check for HTML comment
		if pos+3 < llen && lower[pos:pos+4] == "<!--" {
			end := strings.Index(lower[pos:], "-->")
			if end < 0 {
				break
			}
			pos += end + 3
			continue
		}

		// Check for skip-tag open
		if lower[pos] == '<' {
			skipped := false
			for _, tag := range skipTags {
				open := "<" + tag
				if pos+len(open) <= llen && lower[pos:pos+len(open)] == open {
					// find end of this tag block
					close := "</" + tag
					end := strings.Index(lower[pos+len(open):], close)
					if end >= 0 {
						closeEnd := strings.Index(lower[pos+len(open)+end+len(close):], ">")
						if closeEnd >= 0 {
							pos = pos + len(open) + end + len(close) + closeEnd + 1
						} else {
							pos = pos + len(open) + end + len(close)
						}
					} else {
						// no closing tag found — skip to end of opening tag
						end2 := strings.Index(lower[pos:], ">")
						if end2 >= 0 {
							pos += end2 + 1
						} else {
							pos++
						}
					}
					skipped = true
					break
				}
			}
			if skipped {
				continue
			}
			// Regular tag — skip to >
			end := strings.Index(lower[pos:], ">")
			if end < 0 {
				break
			}
			// Add newline after block-level tags for readability
			tag := strings.TrimLeft(lower[pos+1:pos+min(pos+20, llen)-pos], "/")
			if len(tag) > 0 {
				t := strings.Fields(tag)
				if len(t) > 0 {
					switch t[0] {
					case "p", "div", "h1", "h2", "h3", "h4", "h5", "h6", "li", "br", "tr", "td", "section", "article":
						b.WriteString(" ")
					}
				}
			}
			pos += end + 1
			continue
		}

		// Regular character
		b.WriteByte(html[pos])
		pos++
	}

	// Collapse whitespace and HTML entities
	result := b.String()
	result = strings.ReplaceAll(result, "&nbsp;", " ")
	result = strings.ReplaceAll(result, "&amp;", "&")
	result = strings.ReplaceAll(result, "&lt;", "<")
	result = strings.ReplaceAll(result, "&gt;", ">")
	result = strings.ReplaceAll(result, "&quot;", "\"")
	result = strings.ReplaceAll(result, "&#39;", "'")

	// Collapse runs of whitespace/newlines
	lines := strings.Split(result, "\n")
	var cleaned []string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		// collapse multiple spaces
		for strings.Contains(line, "  ") {
			line = strings.ReplaceAll(line, "  ", " ")
		}
		if line != "" {
			cleaned = append(cleaned, line)
		}
	}
	return strings.Join(cleaned, "\n")
}

func extractTagContent(html, lower, tag string) string {
	open := "<" + tag
	close := "</" + tag + ">"
	start := strings.Index(lower, open)
	if start < 0 {
		return ""
	}
	gt := strings.Index(lower[start:], ">")
	if gt < 0 {
		return ""
	}
	contentStart := start + gt + 1
	end := strings.Index(lower[contentStart:], close)
	if end < 0 {
		return ""
	}
	return html[contentStart : contentStart+end]
}

// stripHTML is kept for backward compatibility.
func stripHTML(s string) string {
	return extractPageText(s)
}

// cleanJSON strips markdown code fences from LLM output.
func cleanJSON(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "```") {
		nl := strings.Index(s, "\n")
		if nl >= 0 {
			s = s[nl+1:]
		}
		s = strings.TrimSuffix(strings.TrimSpace(s), "```")
	}
	return strings.TrimSpace(s)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// ── Product image upload ──────────────────────────────────────────────────────

// POST /api/products/{id}/images
// @Summary     Upload product image
// @Description Uploads an image file and appends it to the product's manual_images list.
// @Tags        products
// @Accept      multipart/form-data
// @Param       id     path  int     true  "Product ID"
// @Param       file   formData  file    true  "Image file"
// @Param       label  formData  string  false "Human-readable label used by AI for matching"
// @Success     201  {object}  db.ProductImage
// @Failure     400  {object}  ErrorResponse
// @Failure     500  {object}  ErrorResponse
// @Router      /api/products/{id}/images [post]
func (s *Server) uploadProductImage(w http.ResponseWriter, r *http.Request) {
	if !s.requirePermission(w, r, "products.manage") {
		return
	}
	id, err := parseID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := r.ParseMultipartForm(10 << 20); err != nil {
		writeError(w, http.StatusBadRequest, "invalid multipart form")
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "file field required")
		return
	}
	defer file.Close()

	label := strings.TrimSpace(r.FormValue("label"))
	if label == "" {
		base := filepath.Base(header.Filename)
		label = strings.TrimSuffix(base, filepath.Ext(base))
		label = strings.NewReplacer("-", " ", "_", " ").Replace(label)
	}

	ext := filepath.Ext(header.Filename)
	filename := fmt.Sprintf("%d%s", time.Now().UnixNano(), ext)

	dir := filepath.Join(s.cfg.RecordingsDir, "..", "product_images")
	if mkErr := os.MkdirAll(dir, 0755); mkErr != nil {
		writeError(w, http.StatusInternalServerError, "storage error")
		return
	}
	dst, err := os.Create(filepath.Join(dir, filename))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "storage error")
		return
	}
	defer dst.Close()
	if _, err := io.Copy(dst, file); err != nil {
		writeError(w, http.StatusInternalServerError, "write error")
		return
	}

	publicURL := strings.TrimRight(s.cfg.PublicServerURL, "/") + "/api/product-images/" + filename

	product, ok := s.authorizeProduct(w, r, id)
	if !ok {
		return
	}
	newImage := db.ProductImage{URL: publicURL, Label: label}
	images := append(product.ManualImages, newImage)
	if err := s.db.UpdateProductManualImages(id, images); err != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}
	writeJSON(w, http.StatusCreated, newImage)
}

// PUT /api/products/{id}/images
// @Summary     Replace all manual images
// @Description Replaces the full manual_images array (used to update labels or reorder).
// @Tags        products
// @Param       id    path  int                  true  "Product ID"
// @Param       body  body  []db.ProductImage    true  "Full images array"
// @Success     204  "No Content"
// @Router      /api/products/{id}/images [put]
func (s *Server) updateProductImages(w http.ResponseWriter, r *http.Request) {
	if !s.requirePermission(w, r, "products.manage") {
		return
	}
	id, err := parseID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if _, ok := s.authorizeProduct(w, r, id); !ok {
		return
	}
	var images []db.ProductImage
	if err := json.NewDecoder(r.Body).Decode(&images); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if err := s.db.UpdateProductManualImages(id, images); err != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// DELETE /api/products/{id}/images/{index}
// @Summary     Delete product image
// @Description Removes a manually uploaded image by its index in the manual_images list.
// @Tags        products
// @Param       id     path  int  true  "Product ID"
// @Param       index  path  int  true  "Zero-based index in manual_images array"
// @Success     204  "No Content"
// @Failure     400  {object}  ErrorResponse
// @Failure     404  {object}  ErrorResponse
// @Router      /api/products/{id}/images/{index} [delete]
func (s *Server) deleteProductImage(w http.ResponseWriter, r *http.Request) {
	if !s.requirePermission(w, r, "products.manage") {
		return
	}
	id, err := parseID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	product, ok := s.authorizeProduct(w, r, id)
	if !ok {
		return
	}
	idx, err := strconv.Atoi(r.PathValue("index"))
	if err != nil || idx < 0 {
		writeError(w, http.StatusBadRequest, "invalid index")
		return
	}
	if idx >= len(product.ManualImages) {
		writeError(w, http.StatusBadRequest, "index out of range")
		return
	}
	images := append(product.ManualImages[:idx:idx], product.ManualImages[idx+1:]...)
	if err := s.db.UpdateProductManualImages(id, images); err != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// GET /api/product-images/{filename}
// @Summary     Serve product image
// @Description Serves an uploaded product image file (public, no auth required — URLs are sent via WhatsApp).
// @Tags        products
// @Param       filename  path  string  true  "Image filename"
// @Success     200  "Image file"
// @Failure     400  {object}  ErrorResponse
// @Router      /api/product-images/{filename} [get]
func (s *Server) serveProductImage(w http.ResponseWriter, r *http.Request) {
	filename := r.PathValue("filename")
	if strings.Contains(filename, "/") || strings.Contains(filename, "..") {
		writeError(w, http.StatusBadRequest, "invalid filename")
		return
	}
	dir := filepath.Join(s.cfg.RecordingsDir, "..", "product_images")
	http.ServeFile(w, r, filepath.Join(dir, filename))
}
