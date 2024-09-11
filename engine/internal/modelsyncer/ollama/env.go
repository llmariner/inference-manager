package ollama

import (
	"log"
	"os"
	"strconv"

	"github.com/llm-operator/inference-manager/engine/internal/config"
)

// SetEnvVarsFromConfig sets environment variables from the given configuration.
func SetEnvVarsFromConfig(c config.OllamaConfig) error {
	// Ollama creaets a payload in a temporary directory by default, and a new temporary directory is created
	// whenever Ollama restarts. This is a problem when a persistent volume is mounted.
	// To avoid this, we set the directory to a fixed path.
	//
	// TODO(kenji): Make sure there is no issue when multiple pods start at the same time.
	if err := os.Setenv("OLLAMA_RUNNERS_DIR", c.RunnersDir); err != nil {
		return err
	}

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
