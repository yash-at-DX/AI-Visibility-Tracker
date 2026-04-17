package models

import "database/sql"

type Result struct {
	ProjectID     string         `json:"project_id"`
	Query         string         `json:"query"`
	Category      sql.NullString `json:"category"`
	Intent        sql.NullString `json:"intent"`
	Source        string         `json:"source"`
	InternalLinks []string       `json:"internal_links"`
	SearchVolume  int            `json:"search_volume"`
	Timestamp     int64          `json:"timestamp"`
}
