package main

import (
	"log"
	"os"
	"strings"

	"github.com/joho/godotenv"
	"github.com/yash-at-DX/ai-scraper/internal/service"
	"github.com/yash-at-DX/ai-scraper/internal/storage"
)

func main() {
	// Subcommand dispatch. Bare invocation = cron flow; "ondemand" = on-demand flow.
	if len(os.Args) > 1 && os.Args[1] == "ondemand" {
		runOnDemand()
		return
	}

	runCron()
}

func runCron() {
	if envFile := os.Getenv("ENV_FILE"); envFile != "" {
		if err := godotenv.Overload(envFile); err != nil {
			log.Fatalf("failed to load env file %q: %v", envFile, err)
		}
	}

	if err := storage.InitDB(); err != nil {
		log.Fatal("Failed to initialize db: ", err)
	}

	// Optional filters from env (set by run_all.sh or manually).
	projectID := strings.TrimSpace(os.Getenv("PROJECT_ID"))
	platforms := parsePlatforms(os.Getenv("PLATFORMS"))

	if projectID != "" {
		log.Printf("Project filter : %s\n", projectID)
	} else {
		log.Println("Project filter : all")
	}
	if len(platforms) > 0 {
		log.Printf("Platform filter: %s\n", strings.Join(platforms, ", "))
	} else {
		log.Println("Platform filter: all")
	}

	queries, err := storage.GetVisibiltyQueries(projectID)
	if err != nil {
		log.Fatal("failed to fetch queries: ", err)
	}
	log.Printf("Found %d rows from DB\n", len(queries))

	service.RunAllScrapers(queries, platforms)

	log.Println("Done. Exiting...")
}

// parsePlatforms splits a comma-separated platform string into a slice.
// Empty/blank input returns nil (= all platforms).
func parsePlatforms(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	var out []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
