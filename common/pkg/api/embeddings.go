package api

import (
	"encoding/base64"
	"encoding/json"
)

// ConvertCreateEmbeddingRequestToProto converts the request to the protobuf format.
func ConvertCreateEmbeddingRequestToProto(body []byte) ([]byte, error) {
	fs := []convertF{
		convertInput,
	}
	return applyConvertFuncs(body, fs)
}

// ConvertCreateEmbeddingRequestToOpenAI converts the request to the OpenAI format.
func ConvertCreateEmbeddingRequestToOpenAI(body []byte) ([]byte, error) {
	fs := []convertF{
		convertEncodedInput,
	}
	return applyConvertFuncs(body, fs)
}

// convertInput checks if the value of the "input" is string, and
// takes the following convertion if it is not a string.
// 1. Encoded the value and store it in the "encoded_input" field.
// 2. Remove the "input" field.
func convertInput(r map[string]interface{}) error {
	input, ok := r["input"]
	if !ok {
		return nil
	}

	if _, ok := input.(string); ok {
		return nil
	}

	mi, err := json.Marshal(input)
	if err != nil {
		return err
	}

	r["encoded_input"] = base64.URLEncoding.EncodeToString(mi)
	delete(r, "input")

	return nil
}

func convertEncodedInput(r map[string]interface{}) error {
	input, ok := r["encoded_input"]
	if !ok {
		return nil
	}

	b, err := base64.URLEncoding.DecodeString(input.(string))
	if err != nil {
		return err

	}

	i := []interface{}{}
	if err := json.Unmarshal(b, &i); err != nil {
		return err
	}

	r["input"] = i
	delete(r, "encoded_input")

	return nil
}
