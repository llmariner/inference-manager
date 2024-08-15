package ollama

import (
	"log"
	"os"
	"strconv"

	"github.com/llm-operator/inference-manager/engine/internal/config"
)

// SetEnvVarsFromConfig sets environment variables from the given configuration.
func SetEnvVarsFromConfig(c config.OllamaConfig) error {
	if err := os.Setenv("OLLAMA_KEEP_ALIVE", c.KeepAlive.String()); err != nil {
		return err
	}

	if c.NumParallel > 0 {
		log.Printf("Setting Ollama NumParallel %d\n", c.NumParallel)
		if err := os.Setenv("OLLAMA_NUM_PARALLEL", strconv.Itoa(c.NumParallel)); err != nil {
			return err
		}
	}

	if c.ForceSpreading {
		log.Printf("Enabling Ollama Force spreading.\n")
		if err := os.Setenv("OLLAMA_SCHED_SPREAD", "true"); err != nil {
			return err
		}
	}

	if c.Debug {
		log.Printf("Enabling Ollama debug mode.\n")
		if err := os.Setenv("OLLAMA_DEBUG", "true"); err != nil {
			return err
		}
	}

	return nil
}
