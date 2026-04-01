package storage

import (
	"encoding/json"
	"log"
	"strings"

	"github.com/yash-at-DX/ai-scraper/internal/models"
)

func InsertResults(results []models.Result) error {
	if len(results) == 0 {
		return nil
	}
	baseQuery := `
	INSERT INTO ai_visibility (query, source, internal_links)
	VALUES `
	vals := []interface{}{}
	placeholders := []string{}

	for _, r := range results {
		placeholders = append(placeholders, "(?,?,?)")

		linksJson, _ := json.Marshal(r.InternalLinks)

		vals = append(vals, r.Query, r.Source, linksJson)
	}

	query := baseQuery + strings.Join(placeholders, ",")

	_, err := DB.Exec(query, vals...)
	if err != nil {
		log.Println("Final Query: ", query)
		return err
	}
	return nil
}
