package gateway

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

func addChatTools(payload map[string]any, rawTools, rawChoice json.RawMessage, parallel *bool) error {
	if !emptyCompatibilityJSON(rawTools) {
		var tools []struct {
			Type     string `json:"type"`
			Function struct {
				Name        string         `json:"name"`
				Description string         `json:"description,omitempty"`
				Parameters  map[string]any `json:"parameters,omitempty"`
				Strict      *bool          `json:"strict,omitempty"`
			} `json:"function"`
		}
		if err := json.Unmarshal(rawTools, &tools); err != nil {
			return fmt.Errorf("tools must be an array")
		}
		converted := make([]any, 0, len(tools))
		for index, tool := range tools {
			if tool.Type != "function" || strings.TrimSpace(tool.Function.Name) == "" {
				return fmt.Errorf("tools[%d] must be a named function", index)
			}
			function := map[string]any{"type": "function", "name": tool.Function.Name}
			if tool.Function.Description != "" {
				function["description"] = tool.Function.Description
			}
			if tool.Function.Parameters != nil {
				function["parameters"] = tool.Function.Parameters
			}
			if tool.Function.Strict != nil {
				function["strict"] = *tool.Function.Strict
			}
			converted = append(converted, function)
		}
		payload["tools"] = converted
	}
	if err := addChatToolChoice(payload, rawChoice); err != nil {
		return err
	}
	if parallel != nil {
		payload["parallel_tool_calls"] = *parallel
	}
	return nil
}

func addChatToolChoice(payload map[string]any, raw json.RawMessage) error {
	if emptyCompatibilityJSON(raw) {
		return nil
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return fmt.Errorf("tool_choice is invalid")
	}
	if choice, ok := value.(string); ok {
		payload["tool_choice"] = choice
		return nil
	}
	choice, ok := value.(map[string]any)
	if !ok || choice["type"] != "function" {
		return fmt.Errorf("tool_choice must be auto, none, required, or a named function")
	}
	function, ok := choice["function"].(map[string]any)
	name, _ := function["name"].(string)
	if !ok || strings.TrimSpace(name) == "" {
		return fmt.Errorf("tool_choice.function.name is required")
	}
	payload["tool_choice"] = map[string]any{"type": "function", "name": name}
	return nil
}

func addAnthropicTools(payload map[string]any, rawTools, rawChoice json.RawMessage) error {
	if !emptyCompatibilityJSON(rawTools) {
		var tools []struct {
			Name        string         `json:"name"`
			Description string         `json:"description,omitempty"`
			InputSchema map[string]any `json:"input_schema"`
		}
		if err := json.Unmarshal(rawTools, &tools); err != nil {
			return fmt.Errorf("tools must be an array")
		}
		converted := make([]any, 0, len(tools))
		for index, tool := range tools {
			if strings.TrimSpace(tool.Name) == "" {
				return fmt.Errorf("tools[%d].name is required", index)
			}
			function := map[string]any{"type": "function", "name": tool.Name, "parameters": tool.InputSchema}
			if tool.Description != "" {
				function["description"] = tool.Description
			}
			converted = append(converted, function)
		}
		payload["tools"] = converted
	}
	if emptyCompatibilityJSON(rawChoice) {
		return nil
	}
	var choice struct {
		Type string `json:"type"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal(rawChoice, &choice); err != nil {
		return fmt.Errorf("tool_choice is invalid")
	}
	switch choice.Type {
	case "auto", "none":
		payload["tool_choice"] = choice.Type
	case "any":
		payload["tool_choice"] = "required"
	case "tool":
		if strings.TrimSpace(choice.Name) == "" {
			return fmt.Errorf("tool_choice.name is required")
		}
		payload["tool_choice"] = map[string]any{"type": "function", "name": choice.Name}
	default:
		return fmt.Errorf("tool_choice.type is unsupported")
	}
	return nil
}

func encodeCompatibilityToolOutput(value any) string {
	if value == nil {
		return ""
	}
	if text, ok := value.(string); ok {
		return text
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return fmt.Sprint(value)
	}
	return string(encoded)
}

func emptyCompatibilityJSON(raw json.RawMessage) bool {
	value := bytes.TrimSpace(raw)
	return len(value) == 0 || bytes.Equal(value, []byte("null"))
}
