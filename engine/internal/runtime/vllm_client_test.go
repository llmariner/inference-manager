package runtime

import (
	"context"
	"testing"

	"github.com/llmariner/inference-manager/engine/internal/config"
	mv1 "github.com/llmariner/model-manager/api/v1"
	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc"
)

func TestDeployRuntimeParams(t *testing.T) {
	commonClient := &commonClient{
		mconfig: config.NewProcessedModelConfig(&config.Config{
			Model: config.ModelConfig{
				Overrides: map[string]config.ModelConfigItem{
					"TinyLlama-TinyLlama-1.1B-Chat-v1.0": {
						Resources: config.Resources{
							Limits: map[string]string{
								"cpu":            "1",
								"nvidia.com/gpu": "2",
							},
						},
					},
				},
			},
		}),
	}

	tcs := []struct {
		name     string
		modelID  string
		resp     *mv1.GetBaseModelPathResponse
		attr     *mv1.ModelAttributes
		model    *mv1.Model
		wantArgs []string
	}{
		{
			name:    "both formats",
			modelID: "TinyLlama-TinyLlama-1.1B-Chat-v1.0",
			resp: &mv1.GetBaseModelPathResponse{
				Path: "path",
				Formats: []mv1.ModelFormat{
					mv1.ModelFormat_MODEL_FORMAT_GGUF,
					mv1.ModelFormat_MODEL_FORMAT_HUGGING_FACE,
				},
			},
			attr: &mv1.ModelAttributes{
				Quantization: mv1.QuantizationType_QUANTIZATION_TYPE_GGUF,
				BaseModel:    "base-model0",
				Path:         "/models/TinyLlama-TinyLlama-1.1B-Chat-v1.0",
			},
			model: &mv1.Model{
				Id:          "TinyLlama-TinyLlama-1.1B-Chat-v1.0",
				IsBaseModel: false,
			},
			wantArgs: []string{
				"--port", "80",
				"--model", "/models/TinyLlama-TinyLlama-1.1B-Chat-v1.0",
				"--served-model-name", "TinyLlama-TinyLlama-1.1B-Chat-v1.0",
				"--chat-template", "\n<|begin_of_text|>\n{% for message in messages %}\n{{'<|start_header_id|>' + message['role'] + '<|end_header_id|>\\n' + message['content'] + '\\n<|eot_id|>\\n'}}\n{% endfor %}\n",
				"--tensor-parallel-size", "2",
			},
		},
		{
			name:    "gguf only",
			modelID: "TinyLlama-TinyLlama-1.1B-Chat-v1.0",
			resp: &mv1.GetBaseModelPathResponse{
				Path: "path",
				Formats: []mv1.ModelFormat{
					mv1.ModelFormat_MODEL_FORMAT_GGUF,
				},
			},
			attr: &mv1.ModelAttributes{
				Quantization: mv1.QuantizationType_QUANTIZATION_TYPE_GGUF,
				BaseModel:    "base-model0",
				Path:         "/models/TinyLlama-TinyLlama-1.1B-Chat-v1.0/model.gguf",
			},
			model: &mv1.Model{
				Id:          "TinyLlama-TinyLlama-1.1B-Chat-v1.0",
				IsBaseModel: false,
			},
			wantArgs: []string{
				"--port", "80",
				"--model", "/models/TinyLlama-TinyLlama-1.1B-Chat-v1.0/model.gguf",
				"--served-model-name", "TinyLlama-TinyLlama-1.1B-Chat-v1.0",
				"--chat-template", "\n<|begin_of_text|>\n{% for message in messages %}\n{{'<|start_header_id|>' + message['role'] + '<|end_header_id|>\\n' + message['content'] + '\\n<|eot_id|>\\n'}}\n{% endfor %}\n",
				"--tensor-parallel-size", "2",
			},
		},
		{
			name:    "base model",
			modelID: "TinyLlama-TinyLlama-1.1B-Chat-v1.0",
			resp: &mv1.GetBaseModelPathResponse{
				Path: "path",
				Formats: []mv1.ModelFormat{
					mv1.ModelFormat_MODEL_FORMAT_GGUF,
				},
			},
			model: &mv1.Model{
				Id:          "TinyLlama-TinyLlama-1.1B-Chat-v1.0",
				IsBaseModel: true,
			},
			wantArgs: []string{
				"--port", "80",
				"--model", "/models/TinyLlama-TinyLlama-1.1B-Chat-v1.0/model.gguf",
				"--served-model-name", "TinyLlama-TinyLlama-1.1B-Chat-v1.0",
				"--chat-template", "\n<|begin_of_text|>\n{% for message in messages %}\n{{'<|start_header_id|>' + message['role'] + '<|end_header_id|>\\n' + message['content'] + '\\n<|eot_id|>\\n'}}\n{% endfor %}\n",
				"--tensor-parallel-size", "2",
			},
		},
		{
			name:    "lora adapter",
			modelID: "TinyLlama-TinyLlama-1.1B-Chat-v1.0",
			resp: &mv1.GetBaseModelPathResponse{
				Path: "path",
				Formats: []mv1.ModelFormat{
					mv1.ModelFormat_MODEL_FORMAT_HUGGING_FACE,
				},
			},
			attr: &mv1.ModelAttributes{
				Adapter:   mv1.AdapterType_ADAPTER_TYPE_LORA,
				BaseModel: "base-model0",
				Path:      "/models/TinyLlama-TinyLlama-1.1B-Chat-v1.0",
			},
			model: &mv1.Model{
				Id:          "TinyLlama-TinyLlama-1.1B-Chat-v1.0",
				IsBaseModel: false,
			},
			wantArgs: []string{
				"--port", "80",
				"--model", "/models/base-model0",
				"--served-model-name", "base-model0",
				"--chat-template", "\n<|begin_of_text|>\n{% for message in messages %}\n{{'<|start_header_id|>' + message['role'] + '<|end_header_id|>\\n' + message['content'] + '\\n<|eot_id|>\\n'}}\n{% endfor %}\n",
				"--tensor-parallel-size", "2",
				"--enable-lora",
				"--lora-modules", "TinyLlama-TinyLlama-1.1B-Chat-v1.0=/models/TinyLlama-TinyLlama-1.1B-Chat-v1.0",
			},
		},
	}

	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			v := &vllmClient{
				commonClient: commonClient,
				modelClient: &fakeModelClient{
					resp:  tc.resp,
					attr:  tc.attr,
					model: tc.model,
				},
				vLLMConfig: &config.VLLMConfig{},
			}

			got, err := v.deployRuntimeParams(context.Background(), tc.modelID)
			assert.NoError(t, err)
			assert.ElementsMatch(t, tc.wantArgs, got.args)
		})
	}
}

func TestNumGPUs(t *testing.T) {

	tcs := []struct {
		name string
		mci  config.ModelConfigItem
		want int
	}{
		{
			name: "model0",
			mci: config.ModelConfigItem{
				Resources: config.Resources{
					Limits: map[string]string{
						nvidiaGPUResource: "2",
						"cpu":             "4",
					},
				},
			},
			want: 2,
		},
		{
			name: "model1",
			mci: config.ModelConfigItem{
				Resources: config.Resources{
					Limits: map[string]string{
						"cpu": "8",
					},
				},
			},
			want: 0,
		},
		{
			name: "model2",
			mci: config.ModelConfigItem{
				Resources: config.Resources{
					Limits: map[string]string{
						awsNeuroncoreResource: "1",
						"cpu":                 "3",
					},
				},
			},
			want: 1,
		},
	}

	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			got, err := numGPUs(tc.mci)
			assert.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestVLLMQuantization(t *testing.T) {
	tcs := []struct {
		modelID   string
		wantQ     string
		wantFound bool
	}{
		{
			modelID:   "model-awq",
			wantQ:     "awq",
			wantFound: true,
		},
		{
			modelID:   "model-bnb-4bit",
			wantQ:     "bitsandbytes",
			wantFound: true,
		},
		{
			modelID:   "model",
			wantQ:     "",
			wantFound: false,
		},
	}

	for _, tc := range tcs {
		t.Run(tc.modelID, func(t *testing.T) {
			gotQ, gotFound := vllmQuantization(tc.modelID)
			assert.Equal(t, tc.wantQ, gotQ)
			assert.Equal(t, tc.wantFound, gotFound)
		})
	}
}

type fakeModelClient struct {
	resp  *mv1.GetBaseModelPathResponse
	attr  *mv1.ModelAttributes
	model *mv1.Model
}

func (c *fakeModelClient) GetBaseModelPath(ctx context.Context, in *mv1.GetBaseModelPathRequest, opts ...grpc.CallOption) (*mv1.GetBaseModelPathResponse, error) {
	return c.resp, nil
}

func (c *fakeModelClient) GetModelAttributes(ctx context.Context, in *mv1.GetModelAttributesRequest, opts ...grpc.CallOption) (*mv1.ModelAttributes, error) {
	return c.attr, nil
}

func (c *fakeModelClient) GetModel(ctx context.Context, in *mv1.GetModelRequest, opts ...grpc.CallOption) (*mv1.Model, error) {
	return c.model, nil
}
