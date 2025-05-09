package api

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
)

const (
	contentTypeText = "text"

	// chatTemplateKwargsKey is the key for the chat template kwargs.
	chatTemplateKwargsKey = "chat_template_kwargs"
	// encodedChatTemplateKwargsKey is the key for the encoded chat template kwargs.
	encodedChatTemplateKwargsKey = "encoded_chat_template_kwargs"
)

// ConvertCreateChatCompletionRequestToProto converts the request to the protobuf format.
func ConvertCreateChatCompletionRequestToProto(body []byte) ([]byte, error) {
	fs := []convertF{
		convertToolChoice,
		convertFunctionParameters,
		convertChatTemplateKwargs,
		convertContentStringToArray,
	}
	return applyConvertFuncs(body, fs)
}

// ConvertCreateChatCompletionRequestToOpenAI converts the request to the OpenAI format.
func ConvertCreateChatCompletionRequestToOpenAI(body []byte) ([]byte, error) {
	fs := []convertF{
		// The order of the functions is the opposite of the ConvertCreateChatCompletionRequestToProto.
		//
		// We don't have a function that corresponds to convertContentStringToArray as the convertion
		// doesn't break the OpenAI API spec.
		convertEncodedChatTemplateKwargs,
		convertEncodedFunctionParameters,
		convertToolChoiceObject,
	}
	return applyConvertFuncs(body, fs)
}

// convertToolChoice marshals the "tool_choice" field of the request to
// and sets the result to the "encoded_tool_choice" field of the request.
// The original "tool_choice" field is removed.
func convertToolChoice(r map[string]interface{}) error {
	v, ok := r["tool_choice"]
	if !ok {
		return nil
	}

	if _, ok := v.(string); ok {
		return nil
	}

	r["tool_choice_object"] = v
	delete(r, "tool_choice")
	return nil
}

// convertToolChoiceObject converts "tool_choice_object" to "tool_choice".
func convertToolChoiceObject(r map[string]interface{}) error {
	v, ok := r["tool_choice_object"]
	if !ok {
		return nil
	}
	r["tool_choice"] = v
	delete(r, "tool_choice_object")
	return nil
}

// convertFunctionParameters marshals the "parameters" field of the function to
// and sets the result to the "encoded_parameters" field of the function.
// The original "parameters" field is removed.
func convertFunctionParameters(r map[string]interface{}) error {
	tools, ok := r["tools"]
	if !ok {
		return nil
	}
	for _, tool := range tools.([]interface{}) {
		t := tool.(map[string]interface{})
		f, ok := t["function"]
		if !ok {
			continue
		}

		fn := f.(map[string]interface{})

		p, ok := fn["parameters"]
		if !ok {
			continue
		}

		pp, err := json.Marshal(p)
		if err != nil {
			return err
		}

		fn["encoded_parameters"] = base64.URLEncoding.EncodeToString(pp)
		delete(fn, "parameters")
	}
	return nil
}

func convertEncodedFunctionParameters(r map[string]interface{}) error {
	tools, ok := r["tools"]
	if !ok {
		return nil
	}
	for _, tool := range tools.([]interface{}) {
		t := tool.(map[string]interface{})
		f, ok := t["function"]
		if !ok {
			continue
		}

		fn := f.(map[string]interface{})

		p, ok := fn["encoded_parameters"]
		if !ok {
			continue
		}

		b, err := base64.URLEncoding.DecodeString(p.(string))
		if err != nil {
			return err
		}

		pp := map[string]interface{}{}
		if err := json.Unmarshal(b, &pp); err != nil {
			return err
		}

		fn["parameters"] = pp
		delete(fn, "encoded_parameters")
	}
	return nil
}

func convertChatTemplateKwargs(r map[string]interface{}) error {
	v, ok := r[chatTemplateKwargsKey]
	if !ok {
		return nil
	}

	kw, ok := v.(map[string]interface{})
	if !ok {
		return fmt.Errorf("chat_template_kwargs should be a map")
	}

	b, err := json.Marshal(kw)
	if err != nil {
		return err
	}

	r[encodedChatTemplateKwargsKey] = base64.URLEncoding.EncodeToString(b)
	delete(r, chatTemplateKwargsKey)
	return nil
}

func convertEncodedChatTemplateKwargs(r map[string]interface{}) error {
	v, ok := r[encodedChatTemplateKwargsKey]
	if !ok {
		return nil
	}

	b, err := base64.URLEncoding.DecodeString(v.(string))
	if err != nil {
		return err
	}

	kw := map[string]interface{}{}
	if err := json.Unmarshal(b, &kw); err != nil {
		return err
	}

	r[chatTemplateKwargsKey] = kw
	delete(r, encodedChatTemplateKwargsKey)
	return nil
}

// convertContentStringToArray converts the content field from a string to an array.
func convertContentStringToArray(r map[string]interface{}) error {
	msgs, ok := r["messages"]
	if !ok {
		return fmt.Errorf("messages is required")
	}
	for _, msg := range msgs.([]interface{}) {
		m := msg.(map[string]interface{})
		content, ok := m["content"]
		if !ok {
			return fmt.Errorf("content is required")
		}
		if cs, ok := content.(string); ok {
			m["content"] = []map[string]interface{}{
				{
					"type": contentTypeText,
					"text": cs,
				},
			}
		}
	}
	return nil
}
