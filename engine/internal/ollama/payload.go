package ollama

import (
	"log"
	"os"
	"path/filepath"

	"github.com/llm-operator/inference-manager/engine/internal/config"
)

// DeleteOrphanedRunnersDir deletes orphaned payload directories.
func DeleteOrphanedRunnersDir(c config.OllamaConfig) error {
	files, err := filepath.Glob("/tmp/ollama*")
	if err != nil {
		return err
	}

	for _, file := range files {
		if file == c.RunnersDir {
			// Skip the current payload directory.
			continue
		}

		log.Printf("Deleting orphaned payload directory: %s\n", file)
		if err := os.RemoveAll(file); err != nil {
			return err
		}
	}
	return nil
}
