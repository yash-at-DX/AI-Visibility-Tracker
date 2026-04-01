package models

type Result struct {
	Query         string   `json:"query"`
	Source        string   `json:"source"`
	InternalLinks []string `json:"internal_links"`
	Timestamp     int64    `json:"timestamp"`
}
