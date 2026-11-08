package structured

import (
	"encoding/json"
	"fmt"
)

// Schema represents a JSON schema for structured output.
type Schema struct {
	Type       string                 `json:"type"`
	Properties map[string]Property    `json:"properties"`
	Required   []string               `json:"required,omitempty"`
}

// Property represents a property in a schema.
type Property struct {
	Type        string      `json:"type"`
	Description string      `json:"description,omitempty"`
	Enum        []string    `json:"enum,omitempty"`
	Properties  map[string]Property `json:"properties,omitempty"`
}

// Validator validates JSON responses against a schema.
type Validator struct {
	schema Schema
}

// NewValidator creates a new schema validator.
func NewValidator(schema Schema) *Validator {
	return &Validator{schema: schema}
}

// Validate validates JSON data against the schema.
func (v *Validator) Validate(data []byte) (map[string]interface{}, error) {
	var result map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("parse JSON: %w", err)
	}

	// Check required fields
	for _, field := range v.schema.Required {
		if _, ok := result[field]; !ok {
			return nil, fmt.Errorf("missing required field: %s", field)
		}
	}

	// Validate each field
	for key, value := range result {
		if prop, ok := v.schema.Properties[key]; ok {
			if err := v.validateProperty(key, value, prop); err != nil {
				return nil, err
			}
		}
	}

	return result, nil
}

func (v *Validator) validateProperty(name string, value interface{}, prop Property) error {
	switch prop.Type {
	case "string":
		if _, ok := value.(string); !ok {
			return fmt.Errorf("field '%s' must be a string", name)
		}
		if len(prop.Enum) > 0 {
			found := false
			for _, e := range prop.Enum {
				if s, ok := value.(string); ok && s == e {
					found = true
					break
				}
			}
			if !found {
				return fmt.Errorf("field '%s' value '%v' not in enum", name, value)
			}
		}
	case "number", "integer":
		if _, ok := value.(float64); !ok {
			return fmt.Errorf("field '%s' must be a number", name)
		}
	case "boolean":
		if _, ok := value.(bool); !ok {
			return fmt.Errorf("field '%s' must be a boolean", name)
		}
	case "object":
		if obj, ok := value.(map[string]interface{}); ok {
			for k, p := range prop.Properties {
				if pv, ok := obj[k]; ok {
					if err := v.validateProperty(k, pv, p); err != nil {
						return err
					}
				}
			}
		} else {
			return fmt.Errorf("field '%s' must be an object", name)
		}
	case "array":
		if arr, ok := value.([]interface{}); ok {
			for i, item := range arr {
				if itemProp, ok := prop.Properties["items"]; ok {
					if err := v.validateProperty(fmt.Sprintf("%s[%d]", name, i), item, itemProp); err != nil {
						return err
					}
				}
			}
		} else {
			return fmt.Errorf("field '%s' must be an array", name)
		}
	}

	return nil
}

// BuildSchema creates a schema from a struct definition.
func BuildSchema(name string, properties map[string]Property, required []string) Schema {
	return Schema{
		Type:       "object",
		Properties: properties,
		Required:   required,
	}
}
