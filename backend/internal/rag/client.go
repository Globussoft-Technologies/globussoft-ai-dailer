// Package rag provides an HTTP client to the Python RAG microservice.
package rag

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"time"

	"go.uber.org/zap"
)

// Client calls the RAG service over HTTP.
type Client struct {
	baseURL string
	http    *http.Client
	log     *zap.Logger
}

// New creates a RAG Client.
func New(baseURL string, log *zap.Logger) *Client {
	return &Client{
		baseURL: baseURL,
		http:    &http.Client{Timeout: 30 * time.Second},
		log:     log,
	}
}

// RetrieveContext calls GET /retrieve and returns the top-k relevant text.
func (c *Client) RetrieveContext(ctx context.Context, query string, orgID int64, topK int) (string, error) {
	u := fmt.Sprintf("%s/retrieve?org_id=%d&top_k=%d&query=%s",
		c.baseURL, orgID, topK, url.QueryEscape(query))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return "", err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		c.log.Warn("rag: RetrieveContext failed", zap.Error(err))
		return "", nil // soft failure — call continues without RAG context
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("rag: HTTP %d — %s", resp.StatusCode, string(body))
	}
	var result struct {
		Context string `json:"context"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("rag: decode response: %w", err)
	}
	return result.Context, nil
}

// IngestPDF uploads a PDF to the RAG service for indexing.
func (c *Client) IngestPDF(ctx context.Context, orgID int64, filename string, pdfData []byte) error {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, err := mw.CreateFormFile("file", filename)
	if err != nil {
		return err
	}
	if _, err := fw.Write(pdfData); err != nil {
		return err
	}
	mw.Close()

	u := fmt.Sprintf("%s/ingest?org_id=%d", c.baseURL, orgID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, &buf)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("rag ingest: HTTP %d — %s", resp.StatusCode, string(body))
	}
	return nil
}

// RemoveFile removes a file from the RAG index.
func (c *Client) RemoveFile(ctx context.Context, orgID int64, filename string) error {
	u := fmt.Sprintf("%s/knowledge?org_id=%d&filename=%s",
		c.baseURL, orgID, url.QueryEscape(filename))
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, u, nil)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("rag remove: HTTP %d", resp.StatusCode)
	}
	return nil
}

// HealthCheck pings the RAG service. Returns true if healthy.
func (c *Client) HealthCheck(ctx context.Context) bool {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/health", nil)
	if err != nil {
		return false
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}
