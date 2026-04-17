package models

import "database/sql"

type VisibilityQuery struct {
	ProjectID      string
	Query          string
	MissingSources []string
	Category       sql.NullString
	Intent         sql.NullString
	SearchVolume   int
}
