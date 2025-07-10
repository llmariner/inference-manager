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

	schemaKey        = "schema"
	encodedSchemaKey = "encoded_schema"

	temperatureKey      = "temperature"
	isTemperatureSetKey = "is_temperature_set"

	topPKey      = "top_p"
	isTopPSetKey = "is_top_p_set"
)

// ConvertCreateChatCompletionRequestToProto converts the request to the protobuf format.
func ConvertCreateChatCompletionRequestToProto(body []byte) ([]byte, error) {
	fs := []convertF{
		convertToolChoice,
		convertFunctionParameters,
		convertChatTemplateKwargs,
		convertTemperature,
		convertTopP,
		convertResponseFormat,
		convertContentStringToArray,
	}
	return applyConvertFuncs(body, fs)
}

// ConvertCreateChatCompletionRequestToOpenAI converts the request to the OpenAI format.
func ConvertCreateChatCompletionRequestToOpenAI(body []byte, needStringFormat bool) ([]byte, error) {
	fs := []convertF{
		// The order of the functions is the opposite of the ConvertCreateChatCompletionRequestToProto.
		//
		// We don't have a function that corresponds to convertContentStringToArray as the convertion
		// doesn't break the OpenAI API spec.
		convertEncodedTopP,
		convertEncodedTopP,
		convertEncodedTemperature,
		convertEncodedChatTemplateKwargs,
		convertEncodedFunctionParameters,
		convertEncodedResponseFormat,
		convertToolChoiceObject,
	}
	if needStringFormat {
		// NIM expects the content field to be a string.
		fs = append([]convertF{convertContentArrayToString}, fs...)
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

func convertTemperature(r map[string]interface{}) error {
	return convertNonProtoDefaultValue(r, temperatureKey, isTemperatureSetKey)
}

func convertEncodedTemperature(r map[string]interface{}) error {
	return convertEncodedNonProtoDefaultValue(r, temperatureKey, isTemperatureSetKey)
}

func convertTopP(r map[string]interface{}) error {
	return convertNonProtoDefaultValue(r, topPKey, isTopPSetKey)
}

func convertEncodedTopP(r map[string]interface{}) error {
	return convertEncodedNonProtoDefaultValue(r, topPKey, isTopPSetKey)
}

func convertNonProtoDefaultValue(r map[string]interface{}, key, setKey string) error {
	if _, ok := r[key]; !ok {
		return nil
	}

	r[setKey] = true
	return nil
}

func convertEncodedNonProtoDefaultValue(r map[string]interface{}, key, setKey string) error {
	v, ok := r[setKey]
	if !ok {
		return nil
	}

	set, ok := v.(bool)
	if !ok {
		return fmt.Errorf("%s should be a boolean", setKey)
	}

	delete(r, setKey)

	if !set {
		return nil
	}

	// If the setKey (e.g., "is_temperature_set") is true, but there is no key (e.g., "temperature") field set in proto,
	// explicitly set it to 0.0. Otherwise vLLM will set it to 1.0 as that's the default value in the OpenAI API spec.
	if _, ok := r[key]; ok {
		// Do nothing as the key (e.g., "temperature") is already set.
		return nil
	}
	r[key] = 0.0

	return nil
}

func convertResponseFormat(r map[string]interface{}) error {
	responseFormat, ok := r["response_format"]
	if !ok {
		return nil
	}
	m, ok := responseFormat.(map[string]interface{})
	if !ok {
		return fmt.Errorf("response_format should be a map")
	}

	typeV, ok := m["type"]
	if !ok {
		return fmt.Errorf("response_format.type is required")
	}
	typeVString, ok := typeV.(string)
	if !ok {
		return fmt.Errorf("response_format.type should be a string, got %T", typeV)
	}
	switch typeVString {
	case "text", "json_object":
		// Do nothing.
	case "json_schema":
		js, ok := m["json_schema"]
		if !ok {
			return fmt.Errorf("response_format.json_schema is required for type 'json_schema'")
		}
		jsm, ok := js.(map[string]interface{})
		if !ok {
			return fmt.Errorf("response_format.json_schema should be a map, got %T", js)
		}

		v, ok := jsm[schemaKey]
		if !ok {
			return fmt.Errorf("response_format.json_schema.%s is required for type 'json_schema'", schemaKey)
		}
		t := v.(map[string]interface{})
		marshalled, err := json.Marshal(t)
		if err != nil {
			return err
		}

		jsm[encodedSchemaKey] = base64.URLEncoding.EncodeToString(marshalled)
		delete(jsm, schemaKey)
	default:
		return fmt.Errorf("unsupported response_format.type: %s", typeVString)
	}

	return nil
}

func convertEncodedResponseFormat(r map[string]interface{}) error {
	responseFormat, ok := r["response_format"]
	if !ok {
		return nil
	}
	m, ok := responseFormat.(map[string]interface{})
	if !ok {
		return fmt.Errorf("response_format should be a map")
	}

	typeV, ok := m["type"]
	if !ok {
		return fmt.Errorf("response_format.type is required")
	}
	typeVString, ok := typeV.(string)
	if !ok {
		return fmt.Errorf("response_format.type should be a string, got %T", typeV)
	}
	switch typeVString {
	case "text", "json_object":
		// Do nothing.
	case "json_schema":
		js, ok := m["json_schema"]
		if !ok {
			return fmt.Errorf("response_format.json_schema is required for type 'json_schema'")
		}
		jsm, ok := js.(map[string]interface{})
		if !ok {
			return fmt.Errorf("response_format.json_schema should be a map, got %T", js)
		}

		es, ok := jsm[encodedSchemaKey]
		if !ok {
			return fmt.Errorf("response_format.encoded_schema is required for type 'json_schema'")
		}

		s, err := base64.URLEncoding.DecodeString(es.(string))
		if err != nil {
			return err
		}

		sMap := map[string]interface{}{}
		if err := json.Unmarshal(s, &sMap); err != nil {
			return err
		}

		jsm[schemaKey] = sMap
		delete(jsm, encodedSchemaKey)

	default:
		return fmt.Errorf("unsupported response_format.type: %s", typeVString)
	}

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

// convertContentArrayToString converts the content array back to a string for OpenAI format compatibility.
func convertContentArrayToString(r map[string]interface{}) error {
	msgs, ok := r["messages"]
	if !ok {
		return nil
	}

	for _, msg := range msgs.([]interface{}) {
		m := msg.(map[string]interface{})
		content, ok := m["content"]
		if !ok {
			continue
		}

		// If content is already a string, no conversion needed
		if _, ok := content.(string); ok {
			continue
		}

		// If content is an array, convert it to a string format OpenAI expects
		if contentArr, ok := content.([]interface{}); ok && len(contentArr) > 0 {
			// For text-only content, extract just the text
			if len(contentArr) == 1 {
				if contentItem, ok := contentArr[0].(map[string]interface{}); ok {
					if contentType, ok := contentItem["type"].(string); ok && contentType == contentTypeText {
						if text, ok := contentItem["text"].(string); ok {
							m["content"] = text
							continue
						}
					} else {
						// TODO(guangrui): Handle non-text content.
						return fmt.Errorf("unsupported content type: %s", contentType)
					}
				}
			} else {
				// TODO(guangrui): Handle more complex content arrays.
				return fmt.Errorf("content array with multiple items is not supported")
			}
		}
	}
	return nil
}
