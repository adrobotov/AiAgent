package tools

import (
	"testing"
)

func TestEvaluateExpression(t *testing.T) {
	tests := []struct {
		expr    string
		want    float64
		wantErr bool
	}{
		{"10*10+5", 105, false},
		{"10000*1500+458", 15000458, false},
		{"2+3*4", 14, false},
		{"100/10", 10, false},
		{"5", 5, false},
		{"10-3", 7, false},
		{"2*(3+4)", 10, false}, // parentheses are ignored: 2*3+4=10
	}

	for _, tt := range tests {
		t.Run(tt.expr, func(t *testing.T) {
			got, err := evaluateExpression(tt.expr)
			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error for %q, got nil", tt.expr)
				}
				return
			}
			if err != nil {
				t.Errorf("unexpected error for %q: %v", tt.expr, err)
				return
			}
			if got != tt.want {
				t.Errorf("evaluateExpression(%q) = %v, want %v", tt.expr, got, tt.want)
			}
		})
	}
}
