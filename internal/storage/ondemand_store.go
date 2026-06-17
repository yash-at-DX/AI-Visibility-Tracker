package storage

import (
	"encoding/json"
	"log"

	"github.com/yash-at-DX/ai-scraper/internal/models"
)

// InitOnDemandTable ensures the on-demand results table exists. Call once after
// storage.InitDB() in the on-demand command. Kept separate from the cron's
// createTables() so the two flows don't touch each other's schema setup.
func InitOnDemandTable() error {
	return ensureOnDemandTable()
}

// ensureOnDemandTable creates the on-demand results table if it does not exist.
// Schema mirrors ai_visibility, minus the columns on-demand has no data for
// (category, intent, search_volume), with project_id replaced by run_id.
func ensureOnDemandTable() error {
	ddl := `
	CREATE TABLE IF NOT EXISTS ai_visibility_ondemand (
		id INT AUTO_INCREMENT PRIMARY KEY,
		run_id VARCHAR(255),
		query TEXT,
		source VARCHAR(50),
		internal_links JSON,
		created_at DATE DEFAULT (CURDATE()),
		INDEX idx_run_id (run_id)
	);
	`
	if _, err := DB.Exec(ddl); err != nil {
		log.Println("Failed to create ai_visibility_ondemand: ", err)
		return err
	}
	return nil
}

// IsAlreadyInsertedOnDemand reports whether a (run_id, query, source) row
// already exists. Used to enforce within-run dedupe: the same query+source is
// never stored twice under one run_id.
func IsAlreadyInsertedOnDemand(runID, query, source string) (bool, error) {
	var count int
	err := DB.QueryRow(`
		SELECT COUNT(*) FROM ai_visibility_ondemand
		WHERE run_id = ? AND query = ? AND source = ?
	`, runID, query, source).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// InsertOnDemandResult inserts a single on-demand result under runID.
// Callers are expected to have already applied the skip-when-zero-links rule
// and the within-run dedupe check. Kept single-row to make per-result error
// reporting straightforward for the on-demand command.
func InsertOnDemandResult(runID string, r models.Result) error {
	linksJSON, err := json.Marshal(r.InternalLinks)
	if err != nil {
		return err
	}

	_, err = DB.Exec(`
		INSERT INTO ai_visibility_ondemand (run_id, query, source, internal_links)
		VALUES (?, ?, ?, ?)
	`, runID, r.Query, r.Source, linksJSON)
	if err != nil {
		log.Println("On-demand insert failed: ", err)
		return err
	}
	return nil
}
