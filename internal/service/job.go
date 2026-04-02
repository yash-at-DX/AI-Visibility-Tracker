package service

import (
	"fmt"
	"time"
)

func GenerateJobID() string {
	return fmt.Sprintf("job_%d", time.Now().UnixNano())
}
