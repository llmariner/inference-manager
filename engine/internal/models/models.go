package models

import (
	"fmt"
	"strings"
)

// ExtractBaseModel extracts the base model ID from the given model ID.
// TODO(kenji): Deprecate. We should be able to obtain the information from Model Manager Server.
func ExtractBaseModel(modelID string) (string, error) {
	l := strings.Split(modelID, ":")
	if len(l) <= 2 {
		return "", fmt.Errorf("invalid model ID: %q", modelID)
	}
	return strings.Join(l[1:len(l)-1], ":"), nil
}
