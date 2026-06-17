package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"

	"github.com/joho/godotenv"
	"github.com/yash-at-DX/ai-scraper/internal/service"
	"github.com/yash-at-DX/ai-scraper/internal/storage"
)

// onDemandRequest is the stdin JSON contract from the FastAPI caller.
//   run_id    : caller-supplied identifier scoping this batch of results.
//   platforms : which platforms to run; empty/omitted means all.
//   queries   : batch of queries to scrape.
type onDemandRequest struct {
	RunID     string   `json:"run_id"`
	Platforms []string `json:"platforms"`
	Queries   []string `json:"queries"`
}

// onDemandError reports a single (query, source) failure within the run.
type onDemandError struct {
	Query  string `json:"query"`
	Source string `json:"source,omitempty"`
	Error  string `json:"error"`
}

// onDemandStatus is the stdout JSON contract back to the caller.
type onDemandStatus struct {
	RunID            string          `json:"run_id"`
	Status           string          `json:"status"` // "ok" | "partial" | "error"
	QueriesProcessed int             `json:"queries_processed"`
	RowsInserted     int             `json:"rows_inserted"`
	Errors           []onDemandError `json:"errors"`
}

// runOnDemand reads a JSON request from stdin, scrapes the batch, writes
// results to ai_visibility_ondemand, and prints a status JSON to stdout.
// All logging goes to stderr (Go's log default) so stdout carries only the
// status payload. Exits non-zero on a hard failure (bad input, DB init,
// unknown platform) so the caller can distinguish "ran" from "crashed".
func runOnDemand() {
	if envFile := os.Getenv("ENV_FILE"); envFile != "" {
		if err := godotenv.Overload(envFile); err != nil {
			log.Fatalf("failed to load env file %q: %v", envFile, err)
		}
	}

	// ── parse stdin ──────────────────────────────────────────────────────────
	var req onDemandRequest
	if err := json.NewDecoder(os.Stdin).Decode(&req); err != nil {
		failHard("", fmt.Sprintf("failed to parse stdin JSON: %v", err))
	}
	if req.RunID == "" {
		failHard("", "run_id is required")
	}
	if len(req.Queries) == 0 {
		failHard(req.RunID, "queries must be non-empty")
	}

	// Validate platforms up front so a typo fails the whole run before any
	// browser starts, rather than silently scraping nothing.
	if _, err := service.ResolvePlatforms(req.Platforms); err != nil {
		failHard(req.RunID, err.Error())
	}

	// Dedupe the input batch so an identical query isn't scraped twice.
	queries := dedupeStrings(req.Queries)

	// ── db ───────────────────────────────────────────────────────────────────
	if err := storage.InitDB(); err != nil {
		failHard(req.RunID, fmt.Sprintf("failed to initialize db: %v", err))
	}
	if err := storage.InitOnDemandTable(); err != nil {
		failHard(req.RunID, fmt.Sprintf("failed to ensure on-demand table: %v", err))
	}

	// ── scrape batch ─────────────────────────────────────────────────────────
	status := onDemandStatus{RunID: req.RunID, Errors: []onDemandError{}}

	for _, q := range queries {
		status.QueriesProcessed++
		log.Printf("on-demand: processing query %q\n", q)

		results, err := service.RunQuery(context.Background(), q, req.Platforms)
		if err != nil {
			// Should not happen post-validation, but report and continue.
			status.Errors = append(status.Errors, onDemandError{Query: q, Error: err.Error()})
			continue
		}

		for _, res := range results {
			// skip-when-zero-links rule (mirrors the cron flow)
			if len(res.InternalLinks) == 0 {
				log.Printf("[%s] no links for %q, skipping insert\n", res.Source, q)
				continue
			}

			// within-run dedupe on (run_id, query, source)
			already, derr := storage.IsAlreadyInsertedOnDemand(req.RunID, res.Query, res.Source)
			if derr != nil {
				status.Errors = append(status.Errors, onDemandError{
					Query: q, Source: res.Source,
					Error: fmt.Sprintf("dedupe check failed: %v", derr),
				})
				continue
			}
			if already {
				log.Printf("[%s] already inserted for run %s / %q, skipping\n", res.Source, req.RunID, q)
				continue
			}

			if ierr := storage.InsertOnDemandResult(req.RunID, res); ierr != nil {
				status.Errors = append(status.Errors, onDemandError{
					Query: q, Source: res.Source,
					Error: fmt.Sprintf("insert failed: %v", ierr),
				})
				continue
			}
			status.RowsInserted++
			log.Printf("[%s] inserted %d links for %q\n", res.Source, len(res.InternalLinks), q)
		}
	}

	if len(status.Errors) == 0 {
		status.Status = "ok"
	} else {
		status.Status = "partial"
	}

	emitStatus(status)
}

// failHard prints an error status to stdout and exits non-zero.
func failHard(runID, msg string) {
	emitStatus(onDemandStatus{
		RunID:  runID,
		Status: "error",
		Errors: []onDemandError{{Error: msg}},
	})
	os.Exit(1)
}

// emitStatus writes the status JSON to stdout (the only thing that goes there).
func emitStatus(s onDemandStatus) {
	enc := json.NewEncoder(os.Stdout)
	if err := enc.Encode(s); err != nil {
		// Last resort: at least signal failure on stderr.
		log.Printf("failed to encode status JSON: %v\n", err)
	}
}

func dedupeStrings(in []string) []string {
	seen := make(map[string]bool, len(in))
	var out []string
	for _, s := range in {
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}
