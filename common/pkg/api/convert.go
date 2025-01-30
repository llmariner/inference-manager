package api

import (
	"encoding/json"
	"fmt"
)

type convertF func(map[string]interface{}) error

func applyConvertFuncs(body []byte, fs []convertF) ([]byte, error) {
	r := map[string]interface{}{}
	if err := json.Unmarshal([]byte(body), &r); err != nil {
		return nil, err
	}

	for _, f := range fs {
		if err := f(r); err != nil {
			return nil, err
		}
	}

	body, err := json.Marshal(r)
	if err != nil {
		return nil, fmt.Errorf("marshal the request: %s", err)
	}

	return body, nil
}
