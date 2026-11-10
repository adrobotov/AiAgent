package tools

import "testing"

func TestParseWttr(t *testing.T) {
	data := []byte(`{
		"nearest_area": [{
			"areaName": [{"value": "Volgograd"}],
			"country": [{"value": "Russia"}]
		}],
		"current_condition": [{
			"temp_C": "7",
			"windspeedKmph": "11",
			"weatherDesc": [{"value": "Partly cloudy "}]
		}]
	}`)

	name, line, err := parseWttr(data)
	if err != nil {
		t.Fatalf("parseWttr: %v", err)
	}
	if name != "Volgograd, Russia" {
		t.Fatalf("name = %q", name)
	}
	want := "7°C, Partly cloudy, ветер 11 км/ч"
	if line != want {
		t.Fatalf("line = %q, want %q", line, want)
	}
}

func TestParseWttrEmpty(t *testing.T) {
	if _, _, err := parseWttr([]byte(`{"current_condition":[]}`)); err == nil {
		t.Fatal("expected error for empty current_condition")
	}
	if _, _, err := parseWttr([]byte(`{bad`)); err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}
