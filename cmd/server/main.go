package main

import (
	"log"
	"os"

	"github.com/joho/godotenv"
	"github.com/yash-at-DX/ai-scraper/internal/service"
	"github.com/yash-at-DX/ai-scraper/internal/storage"
)

func main() {
	// Subcommand dispatch. Bare invocation keeps the original cron behavior;
	// "ondemand" runs the on-demand flow (stdin JSON in, status JSON out).
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

	queries, err := storage.GetVisibiltyQueries()
	if err != nil {
		log.Fatal("failed to fetch queries: ", err)
	}
	log.Printf("Found %d rows from DB\n", len(queries))

	service.RunAllScrapers(queries)

	log.Println("Done. Exiting...")
}
