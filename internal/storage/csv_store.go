package storage

import (
	"encoding/csv"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/yash-at-DX/ai-scraper/internal/models"
)

type CSVStore struct {
	FilePath string
	mu       sync.Mutex
}

func (s *CSVStore) Append(results []models.Result) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	file, err := os.OpenFile(s.FilePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer file.Close()

	writer := csv.NewWriter(file)
	defer writer.Flush()

	fileInfo, err := file.Stat()
	if err == nil && fileInfo.Size() == 0 {
		// write header
		err := writer.Write([]string{
			"query",
			"source",
			"content",
			"links",
			"timestamp",
		})
		if err != nil {
			return err
		}
	}

	for _, r := range results {
		record := []string{
			r.Query,
			r.Source,
			r.Content,
			strings.Join(r.InternalLinks, "|"), // join links
			time.Now().Format(time.RFC3339),
		}

		if err := writer.Write(record); err != nil {
			return err
		}
	}

	return nil
}
