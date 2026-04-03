package main

import (
	"encoding/json"
	"log"

	"github.com/joho/godotenv"
	"github.com/yash-at-DX/ai-scraper/internal/models"
	"github.com/yash-at-DX/ai-scraper/internal/service"
	"github.com/yash-at-DX/ai-scraper/internal/storage"
)

func main() {
	if err := godotenv.Load(); err != nil {
		log.Println("Failed to load .env file")
	}

	if err := storage.InitDB(); err != nil {
		log.Fatal("Failed to initialize db: ", err)
	}

	queries, err := storage.GetVisibiltyQueries()
	if err != nil {
		log.Fatal("failed to fetch queries: ", err)
	}
	log.Printf("Found %d rows from DB\n", len(queries))

	expanded := expandQueries(queries)
	log.Printf("Expanded to %d individual queries\n", len(expanded))

	if len(expanded) == 0 {
		log.Println("Nothing to do. Exiting...")
		return
	}

	service.RunAllScrapers(expanded)

	log.Println("Done. Exiting...")
}

func expandQueries(queries []models.VisibilityQuery) []models.VisibilityQuery {
	var expanded []models.VisibilityQuery

	for _, q := range queries {
		var questionList []string

		var inner string
		if err := json.Unmarshal([]byte(q.Query), &inner); err == nil {
			json.Unmarshal([]byte(inner), &questionList)
		}

		if len(questionList) == 0 {
			json.Unmarshal([]byte(q.Query), &questionList)
		}

		if len(questionList) > 0 {
			for _, question := range questionList {
				expanded = append(expanded, models.VisibilityQuery{
					ProjectID: q.ProjectID,
					Query:     question,
				})
			}
		} else {
			expanded = append(expanded, q)
		}
	}

	return expanded
}
