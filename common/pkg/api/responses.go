package api

import (
	"encoding/base64"
	"encoding/json"
)

// ConvertCreateModelResponseRequestToProto converts the request to the protobuf format.
func ConvertCreateModelResponseRequestToProto(body []byte) ([]byte, error) {
	fs := []convertF{
		convertTools,
		convertToolChoiceTools,
		convertToolChoice,
		convertTemperature,
		convertTopP,
		convertTextFormatSchema,
		convertInput,
	}
	return applyConvertFuncs(body, fs)
}

// ConvertCreateModelResponseRequestToOpenAI converts the request to the OpenAI format.
func ConvertCreateModelResponseRequestToOpenAI(body []byte) ([]byte, error) {
	fs := []convertF{
		convertEncodedInput,
		convertEncodedTextFormatSchema,
		convertEncodedTopP,
		convertEncodedTemperature,
		convertToolChoiceObject,
		convertEncodedToolChoiceTools,
		convertEncodedTools,
	}
	return applyConvertFuncs(body, fs)
}

func convertToolChoiceTools(r map[string]interface{}) error {
	toolChoice, ok := r["tool_choice"]
	if !ok {
		return nil
	}
	t, ok := toolChoice.(map[string]interface{})
	if !ok {
		return nil
	}

	tools, ok := t["tools"]
	if !ok {
		return nil
	}
	p, ok := tools.([]interface{})
	if !ok {
		return nil
	}

	pp, err := json.Marshal(p)
	if err != nil {
		return err
	}

	t["encoded_tools"] = base64.URLEncoding.EncodeToString(pp)
	delete(t, "tools")
	return nil
}

func convertEncodedToolChoiceTools(r map[string]interface{}) error {
	toolChoice, ok := r["tool_choice"]
	if !ok {
		return nil
	}

	t, ok := toolChoice.(map[string]interface{})
	if !ok {
		return nil
	}

	p, ok := t["encoded_tools"]
	if !ok {
		return nil
	}

	b, err := base64.URLEncoding.DecodeString(p.(string))
	if err != nil {
		return err
	}

	pp := []interface{}{}
	if err := json.Unmarshal(b, &pp); err != nil {
		return err
	}

	t["tools"] = pp
	delete(t, "encoded_tools")

	return nil
}

func convertTools(r map[string]interface{}) error {
	tools, ok := r["tools"]
	if !ok {
		return nil
	}
	p, ok := tools.([]interface{})
	if !ok {
		return nil
	}

	pp, err := json.Marshal(p)
	if err != nil {
		return err
	}

	r["encoded_tools"] = base64.URLEncoding.EncodeToString(pp)
	delete(r, "tools")
	return nil
}

func convertEncodedTools(r map[string]interface{}) error {
	p, ok := r["encoded_tools"]
	if !ok {
		return nil
	}

	b, err := base64.URLEncoding.DecodeString(p.(string))
	if err != nil {
		return err
	}

	pp := []interface{}{}
	if err := json.Unmarshal(b, &pp); err != nil {
		return err
	}

	r["tools"] = pp
	delete(r, "encoded_tools")

	return nil
}

func convertTextFormatSchema(r map[string]interface{}) error {
	text, ok := r["text"]
	if !ok {
		return nil
	}
	t, ok := text.(map[string]interface{})
	if !ok {
		return nil
	}

	format, ok := t["format"]
	if !ok {
		return nil
	}
	f, ok := format.(map[string]interface{})
	if !ok {
		return nil
	}

	schema, ok := f["schema"]
	if !ok {
		return nil
	}
	s, ok := schema.(map[string]interface{})
	if !ok {
		return nil
	}

	ms, err := json.Marshal(s)
	if err != nil {
		return err
	}

	f["encoded_schema"] = base64.URLEncoding.EncodeToString(ms)
	delete(f, "schema")
	return nil
}

func convertEncodedTextFormatSchema(r map[string]interface{}) error {
	text, ok := r["text"]
	if !ok {
		return nil
	}
	t, ok := text.(map[string]interface{})
	if !ok {
		return nil
	}

	format, ok := t["format"]
	if !ok {
		return nil
	}
	f, ok := format.(map[string]interface{})
	if !ok {
		return nil
	}

	es, ok := f["encoded_schema"]
	if !ok {
		return nil
	}

	b, err := base64.URLEncoding.DecodeString(es.(string))
	if err != nil {
		return err
	}

	s := map[string]interface{}{}
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}

	f["schema"] = s
	delete(f, "encoded_schema")

	return nil
}
