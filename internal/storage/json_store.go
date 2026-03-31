package storage

import (
	"encoding/json"
	"os"
	"sync"
	"time"

	"github.com/yash-at-DX/ai-scraper/internal/models"
)

type JSONStore struct {
	FilePath string
	mu       sync.Mutex
}

func (s *JSONStore) Load() ([]models.Result, error) {
	if _, err := os.Stat(s.FilePath); os.IsNotExist(err) {
		return []models.Result{}, nil
	}

	file, err := os.ReadFile(s.FilePath)
	if err != nil {
		return nil, err
	}

	var data []models.Result
	err = json.Unmarshal(file, &data)
	return data, err
}

func (s *JSONStore) Save(results []models.Result) error {
	data, err := json.MarshalIndent(results, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(s.FilePath, data, 0644)
}

func (s *JSONStore) Upsert(newResults []models.Result) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	existing, err := s.Load()
	if err != nil {
		return err
	}

	dataMap := make(map[string]models.Result)

	// composite key: query + source
	for _, item := range existing {
		key := item.Query + "_" + item.Source
		dataMap[key] = item
	}

	for _, item := range newResults {
		item.Timestamp = time.Now().Unix()
		key := item.Query + "_" + item.Source
		dataMap[key] = item
	}

	final := []models.Result{}
	for _, v := range dataMap {
		final = append(final, v)
	}

	return s.Save(final)
}
