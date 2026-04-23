package api

import (
	"io"
	"net/http"
	"path/filepath"
	"strings"

	"go.uber.org/zap"
)

// GET /api/knowledge
func (s *Server) listKnowledge(w http.ResponseWriter, r *http.Request) {
	ac := getAuth(r)
	files, err := s.db.GetKnowledgeFiles(ac.OrgID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, emptyJSON(files))
}

// POST /api/knowledge  — multipart upload
func (s *Server) uploadKnowledge(w http.ResponseWriter, r *http.Request) {
	ac := getAuth(r)
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		writeError(w, http.StatusBadRequest, "invalid multipart form")
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "file required")
		return
	}
	defer file.Close()

	filename := filepath.Base(header.Filename)
	// Sanitize
	if strings.ContainsAny(filename, "/\\") || strings.Contains(filename, "..") {
		writeError(w, http.StatusBadRequest, "invalid filename")
		return
	}

	ext := strings.ToLower(filepath.Ext(filename))
	if ext != ".pdf" && ext != ".txt" && ext != ".docx" {
		writeError(w, http.StatusBadRequest, "only PDF, TXT, and DOCX files supported")
		return
	}

	data, err := io.ReadAll(io.LimitReader(file, 20<<20)) // 20 MB limit
	if err != nil {
		writeError(w, http.StatusInternalServerError, "read error")
		return
	}

	// Log to DB
	fileID, err := s.db.LogKnowledgeFile(ac.OrgID, filename, ext)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	// Send to RAG service asynchronously
	if s.ragClient != nil {
		go func() {
			if err := s.ragClient.IngestPDF(r.Context(), ac.OrgID, filename, data); err != nil {
				s.logger.Warn("uploadKnowledge: ingest failed",
					zap.String("file", filename), zap.Error(err))
				_ = s.db.UpdateKnowledgeFileStatus(fileID, "failed")
			} else {
				_ = s.db.UpdateKnowledgeFileStatus(fileID, "indexed")
			}
		}()
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"id":       fileID,
		"filename": filename,
		"status":   "processing",
	})
}

// DELETE /api/knowledge/{id}
func (s *Server) deleteKnowledge(w http.ResponseWriter, r *http.Request) {
	ac := getAuth(r)
	id, err := parseID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}

	kf, err := s.db.GetKnowledgeFileByID(id, ac.OrgID)
	if err != nil || kf == nil {
		writeError(w, http.StatusNotFound, "file not found")
		return
	}

	// Remove from RAG index
	if s.ragClient != nil {
		go func() {
			_ = s.ragClient.RemoveFile(r.Context(), ac.OrgID, kf.Filename)
		}()
	}

	if err := s.db.DeleteKnowledgeFile(id, ac.OrgID); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"deleted": true})
}
