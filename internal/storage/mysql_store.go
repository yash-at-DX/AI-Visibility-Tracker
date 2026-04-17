package storage

import (
	"encoding/json"
	"log"
	"strings"

	"github.com/yash-at-DX/ai-scraper/internal/models"
)

func GetVisibiltyQueries() ([]models.VisibilityQuery, error) {
	rows, err := DB.Query(`
		SELECT project_id, query, category, intent, search_volume
		FROM ai_visibility_queries
		WHERE created_at = CURDATE()
	`)
	if err != nil {
		return nil, err
	}

	defer rows.Close()

	var results []models.VisibilityQuery

	for rows.Next() {
		var v models.VisibilityQuery
		if err := rows.Scan(&v.ProjectID, &v.Query, &v.Category, &v.Intent, &v.SearchVolume); err != nil {
			return nil, err
		}
		results = append(results, v)
	}

	return results, nil
}

func InsertResults(results []models.Result) error {
	if len(results) == 0 {
		return nil
	}

	baseQuery := `INSERT INTO ai_visibility (project_id, query, category, intent, source, internal_links, search_volume) VALUES `
	vals := []interface{}{}
	placeholders := []string{}

	for _, r := range results {
		placeholders = append(placeholders, "(?,?,?,?,?,?, ?)")
		linksJson, _ := json.Marshal(r.InternalLinks)
		vals = append(vals, r.ProjectID, r.Query, r.Category, r.Intent, r.Source, linksJson, r.SearchVolume)
	}

	query := baseQuery + strings.Join(placeholders, ",")

	_, err := DB.Exec(query, vals...)
	if err != nil {
		log.Println("Insert failed: ", err)
		return err
	}
	return nil
}

func IsAlreadyScraped(projectID string, query string, source string) (bool, error) {
	var count int
	err := DB.QueryRow(`
		SELECT COUNT(*) FROM ai_visibility
		WHERE project_id = ? AND query = ? AND source = ? AND created_at = CURDATE()
		AND internal_links IS NOT NULL
		AND internal_links != '[]'
		AND internal_links != 'null'
	`, projectID, query, source).Scan(&count)

	if err != nil {
		return false, err
	}

	return count > 0, nil
}
