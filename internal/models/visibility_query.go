package models

type VisibilityQuery struct {
	ProjectID      string
	Query          string
	MissingSources []string
}
