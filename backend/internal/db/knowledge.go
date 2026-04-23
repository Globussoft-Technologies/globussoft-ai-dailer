package db

import (
	"database/sql"
	"errors"
)

// KnowledgeFile mirrors the knowledge_files table.
type KnowledgeFile struct {
	ID       int64  `json:"id"`
	OrgID    int64  `json:"org_id"`
	Filename string `json:"filename"`
	FileType string `json:"file_type"`
	Status   string `json:"status"` // processing, indexed, failed
	CreatedAt string `json:"created_at"`
}

// LogKnowledgeFile inserts a knowledge file record. Returns new ID.
func (d *DB) LogKnowledgeFile(orgID int64, filename, fileType string) (int64, error) {
	res, err := d.pool.Exec(`
		INSERT INTO knowledge_files (org_id, filename, file_type, status)
		VALUES (?,?,?,'processing')`, orgID, filename, fileType)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// UpdateKnowledgeFileStatus updates the indexing status of a file.
func (d *DB) UpdateKnowledgeFileStatus(id int64, status string) error {
	_, err := d.pool.Exec(
		`UPDATE knowledge_files SET status=? WHERE id=?`, status, id)
	return err
}

// GetKnowledgeFiles returns all knowledge files for an org.
func (d *DB) GetKnowledgeFiles(orgID int64) ([]KnowledgeFile, error) {
	rows, err := d.pool.Query(`
		SELECT id, org_id, COALESCE(filename,''), COALESCE(file_type,''),
		COALESCE(status,''), DATE_FORMAT(created_at,'%Y-%m-%d %H:%i:%s')
		FROM knowledge_files WHERE org_id=? ORDER BY id DESC`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []KnowledgeFile
	for rows.Next() {
		var kf KnowledgeFile
		if err := rows.Scan(&kf.ID, &kf.OrgID, &kf.Filename, &kf.FileType,
			&kf.Status, &kf.CreatedAt); err != nil {
			return nil, err
		}
		list = append(list, kf)
	}
	return list, rows.Err()
}

// GetKnowledgeFileByID returns a single knowledge file record.
func (d *DB) GetKnowledgeFileByID(id, orgID int64) (*KnowledgeFile, error) {
	row := d.pool.QueryRow(`
		SELECT id, org_id, COALESCE(filename,''), COALESCE(file_type,''),
		COALESCE(status,''), DATE_FORMAT(created_at,'%Y-%m-%d %H:%i:%s')
		FROM knowledge_files WHERE id=? AND org_id=?`, id, orgID)
	var kf KnowledgeFile
	err := row.Scan(&kf.ID, &kf.OrgID, &kf.Filename, &kf.FileType,
		&kf.Status, &kf.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &kf, err
}

// DeleteKnowledgeFile removes a knowledge file record.
func (d *DB) DeleteKnowledgeFile(id, orgID int64) error {
	_, err := d.pool.Exec(
		`DELETE FROM knowledge_files WHERE id=? AND org_id=?`, id, orgID)
	return err
}
