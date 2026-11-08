package structured

import "testing"

func testSchema() Schema {
	return BuildSchema("person", map[string]Property{
		"name": {Type: "string", Description: "Person name"},
		"age":  {Type: "integer"},
		"role": {Type: "string", Enum: []string{"admin", "user"}},
		"address": {
			Type: "object",
			Properties: map[string]Property{
				"city": {Type: "string"},
			},
		},
		"tags": {Type: "array"},
	}, []string{"name", "age"})
}

func TestValidateSuccess(t *testing.T) {
	v := NewValidator(testSchema())
	data := []byte(`{"name":"Alice","age":30,"role":"admin","address":{"city":"Moscow"},"tags":["a","b"]}`)
	result, err := v.Validate(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result["name"] != "Alice" {
		t.Fatalf("unexpected result: %v", result)
	}
}

func TestValidateMissingRequired(t *testing.T) {
	v := NewValidator(testSchema())
	data := []byte(`{"name":"Alice"}`)
	if _, err := v.Validate(data); err == nil {
		t.Fatal("expected missing required field error")
	}
}

func TestValidateWrongType(t *testing.T) {
	v := NewValidator(testSchema())

	if _, err := v.Validate([]byte(`{"name":123,"age":30}`)); err == nil {
		t.Fatal("expected type error for name")
	}
	if _, err := v.Validate([]byte(`{"name":"A","age":"thirty"}`)); err == nil {
		t.Fatal("expected type error for age")
	}
}

func TestValidateEnum(t *testing.T) {
	v := NewValidator(testSchema())

	if _, err := v.Validate([]byte(`{"name":"A","age":1,"role":"superuser"}`)); err == nil {
		t.Fatal("expected enum violation for role")
	}
	if _, err := v.Validate([]byte(`{"name":"A","age":1,"role":"user"}`)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateBadJSON(t *testing.T) {
	v := NewValidator(testSchema())
	if _, err := v.Validate([]byte(`{not json}`)); err == nil {
		t.Fatal("expected parse error")
	}
}

func TestValidateNestedObject(t *testing.T) {
	v := NewValidator(testSchema())
	data := []byte(`{"name":"A","age":1,"address":{"city":42}}`)
	if _, err := v.Validate(data); err == nil {
		t.Fatal("expected nested type error for address.city")
	}
}

func TestValidateBoolean(t *testing.T) {
	v := NewValidator(BuildSchema("cfg", map[string]Property{
		"enabled": {Type: "boolean"},
	}, nil))

	if _, err := v.Validate([]byte(`{"enabled":true}`)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := v.Validate([]byte(`{"enabled":"yes"}`)); err == nil {
		t.Fatal("expected boolean type error")
	}
}
