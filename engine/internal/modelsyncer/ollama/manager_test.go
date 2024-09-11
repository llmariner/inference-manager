package ollama

import (
	"os"
	"os/exec"
	"testing"

	"github.com/llm-operator/inference-manager/engine/internal/ollama"
	"github.com/stretchr/testify/assert"
)

func TestCreateNewModel(t *testing.T) {
	tcs := []struct {
		name                   string
		modelID                string
		contextLengthByModelID map[string]int
		spec                   *ollama.ModelSpec
		want                   string
	}{
		{
			name:    "gemma",
			modelID: "google-gemma-2b-it-q4",
			spec: &ollama.ModelSpec{
				From: "google-gemma-2b-it-q4",
			},
			want: `FROM google-gemma-2b-it-q4

TEMPLATE """<start_of_turn>user
{{ if .System }}{{ .System }} {{ end }}{{ .Prompt }}<end_of_turn>
<start_of_turn>model
{{ .Response }}<end_of_turn>
"""
PARAMETER repeat_penalty 1
PARAMETER stop "<start_of_turn>"
PARAMETER stop "<end_of_turn>"`,
		},
		{
			name:    "deepseek",
			modelID: "deepseek-ai-deepseek-coder-6.7b-base",
			spec: &ollama.ModelSpec{
				From: "deepseek-ai-deepseek-coder-6.7b-base",
			},
			want: `FROM deepseek-ai-deepseek-coder-6.7b-base

TEMPLATE {{ .Prompt }}
PARAMETER stop <｜end▁of▁sentence｜>
PARAMETER num_ctx 16384
`,
		},
		{
			name:    "deepseek with non-default context length",
			modelID: "deepseek-ai-deepseek-coder-6.7b-base",
			spec: &ollama.ModelSpec{
				From: "deepseek-ai-deepseek-coder-6.7b-base",
			},
			contextLengthByModelID: map[string]int{
				"deepseek-ai-deepseek-coder-6.7b-base": 1024,
			},
			want: `FROM deepseek-ai-deepseek-coder-6.7b-base

TEMPLATE {{ .Prompt }}
PARAMETER stop <｜end▁of▁sentence｜>
PARAMETER num_ctx 1024
`,
		},
		{
			name:    "adapter",
			modelID: "adapter0",
			spec: &ollama.ModelSpec{
				From:        "google-gemma-2b-it-q4",
				AdapterPath: "/path/to/adapter",
			},
			want: `FROM google-gemma-2b-it-q4
Adapter /path/to/adapter
`,
		},
	}

	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			fakeCmdRunner := &fakeCmdRunner{}
			m := &Manager{
				contextLengthsByModelID: tc.contextLengthByModelID,
				cmdRunner:               fakeCmdRunner,
			}
			err := m.CreateNewModelOfGGUF(tc.modelID, tc.spec)
			assert.NoError(t, err)
			assert.Equal(t, tc.want, fakeCmdRunner.gotModeFile)
		})
	}
}

type fakeCmdRunner struct {
	gotModeFile string
}

func (c *fakeCmdRunner) Run(cmd *exec.Cmd) error {
	if len(cmd.Args) < 3 {
		return nil
	}

	if cmd.Args[0] == "ollama" && cmd.Args[1] == "create" {
		b, err := os.ReadFile(cmd.Args[4])
		if err != nil {
			return err
		}
		c.gotModeFile = string(b)
		return nil
	}

	return nil
}
