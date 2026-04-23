package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/globussoft/callified-backend/internal/db"
)

// ── GET /api/organizations ───────────────────────────────────────────────────

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

func (s *Server) deleteOrg(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
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

func (s *Server) updateOrgTimezone(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
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

func (s *Server) getOrgVoiceSettings(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
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

func (s *Server) saveOrgVoiceSettings(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
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

func (s *Server) getOrgSystemPrompt(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	prompt, err := s.db.GetOrgSystemPrompt(id)
	if err != nil {
		s.logger.Sugar().Errorw("getOrgSystemPrompt", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"system_prompt": prompt})
}

// ── PUT /api/organizations/{id}/system-prompt ─────────────────────────────────

func (s *Server) saveOrgSystemPrompt(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	var body struct {
		SystemPrompt string `json:"system_prompt"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if err := s.db.SaveOrgSystemPrompt(id, body.SystemPrompt); err != nil {
		s.logger.Sugar().Errorw("saveOrgSystemPrompt", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"saved": true})
}

// ── GET /api/organizations/{id}/products ─────────────────────────────────────

func (s *Server) listProducts(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
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

func (s *Server) createProduct(w http.ResponseWriter, r *http.Request) {
	orgID, err := parseID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid org id")
		return
	}
	var req productCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" {
		writeError(w, http.StatusBadRequest, "name required")
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

func (s *Server) updateProduct(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
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

func (s *Server) deleteProduct(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
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

func (s *Server) getProductPrompt(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
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

func (s *Server) updateProductPrompt(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
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

// POST /api/products/{id}/scrape
// Fetches the product's website_url and asks the LLM to extract product context.
func (s *Server) scrapeProduct(w http.ResponseWriter, r *http.Request) {
	if s.llmProvider == nil {
		writeError(w, http.StatusServiceUnavailable, "LLM not configured")
		return
	}
	id, err := parseID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	product, err := s.db.GetProductByID(id)
	if err != nil {
		s.logger.Sugar().Errorw("scrapeProduct: GetProductByID", "err", err, "id", id)
		writeError(w, http.StatusInternalServerError, "database error")
		return
	}
	if product == nil {
		writeError(w, http.StatusNotFound, "product not found")
		return
	}
	if product.WebsiteURL == "" {
		writeError(w, http.StatusBadRequest, "product has no website_url")
		return
	}

	// Fetch website (20s timeout, max 500KB)
	fetchCtx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	req2, _ := http.NewRequestWithContext(fetchCtx, http.MethodGet, product.WebsiteURL, nil)
	req2.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
	req2.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req2.Header.Set("Accept-Language", "en-US,en;q=0.9")
	resp, err := http.DefaultClient.Do(req2)
	if err != nil {
		writeError(w, http.StatusBadGateway, "failed to fetch website: "+err.Error())
		return
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 500_000))
	pageText := extractPageText(string(raw))

	if len(strings.TrimSpace(pageText)) < 50 {
		writeError(w, http.StatusBadGateway, "could not extract readable text from website")
		return
	}

	// Trim to 10000 chars for LLM context
	text := pageText
	if len(text) > 10000 {
		text = text[:10000]
	}

	// Ask LLM to extract product info
	prompt := fmt.Sprintf(`You are analyzing a company website to extract product/service information for a sales AI agent.

Extract and summarize:
1. What the product/service is (1-2 sentences)
2. Key features and benefits (bullet points)
3. Target customers / use cases
4. Pricing or plans (if mentioned)
5. Company name and any unique selling points

Be factual and concise (max 300 words). Only include information present on the page.

Website URL: %s
Page content:
%s`, product.WebsiteURL, text)

	s.logger.Sugar().Infow("scrapeProduct: calling LLM", "product_id", id, "textLen", len(text))
	scraped, err := s.llmProvider.GenerateText(r.Context(), prompt, "Extract product info from this website", 2048)
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
	if s.llmProvider == nil {
		writeError(w, http.StatusServiceUnavailable, "LLM not configured")
		return
	}
	id, err := parseID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	product, err := s.db.GetProductByID(id)
	if err != nil || product == nil {
		writeError(w, http.StatusNotFound, "product not found")
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
// Uses Gemini to generate only the agent_persona.
func (s *Server) generateProductPersona(w http.ResponseWriter, r *http.Request) {
	if s.llmProvider == nil {
		writeError(w, http.StatusServiceUnavailable, "LLM not configured")
		return
	}
	id, err := parseID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	product, err := s.db.GetProductByID(id)
	if err != nil || product == nil {
		writeError(w, http.StatusNotFound, "product not found")
		return
	}

	context := strings.TrimSpace(product.ScrapedInfo + "\n" + product.ManualNotes)
	if context == "" {
		context = product.Name
	}

	persona, err := s.llmProvider.GenerateText(r.Context(),
		"Write a concise 2-3 sentence sales agent persona for this product. Be specific about tone and style.",
		"Product: "+context, 512)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "LLM error: "+err.Error())
		return
	}

	if err := s.db.UpdateProductPrompt(id, persona, ""); err != nil {
		s.logger.Sugar().Errorw("generateProductPersona: UpdateProductPrompt", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"agent_persona": persona})
}

// POST /api/organizations/{id}/generate-prompt
// Uses Gemini to generate a custom system prompt for the org.
func (s *Server) generateOrgPrompt(w http.ResponseWriter, r *http.Request) {
	if s.llmProvider == nil {
		writeError(w, http.StatusServiceUnavailable, "LLM not configured")
		return
	}
	id, err := parseID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
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
	llen := len(lower)

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
