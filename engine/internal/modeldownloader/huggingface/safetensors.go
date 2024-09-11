package huggingface

import "encoding/json"

// safetensorsIndex stores the index of safe tensors.
type safetensorsIndex struct {
	WeightMap map[string]string `json:"weight_map"`
}

// unmarshalSafetensorsIndex unmarshals a byte slice into a safetensorsIndex.
func unmarshalSafetensorsIndex(b []byte) (*safetensorsIndex, error) {
	var si safetensorsIndex
	err := json.Unmarshal(b, &si)
	if err != nil {
		return nil, err
	}
	return &si, nil
}
