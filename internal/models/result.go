package models

type Result struct {
	ProjectID     string   `json:"project_id"`
	Query         string   `json:"query"`
	Source        string   `json:"source"`
	InternalLinks []string `json:"internal_links"`
	Timestamp     int64    `json:"timestamp"`
}
