package tools

import "testing"

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
		{"2*(3+4)", 14, false}, // parentheses are honored
		{"(2+3)*4", 20, false}, // parentheses change precedence
		{"10/(2+3)", 2, false}, // division by grouped expr
		{"-5+3", -2, false},    // unary minus
		{"-(3+4)", -7, false},  // unary minus on group
		{"+5-2", 3, false},     // unary plus
		{"2^3", 8, false},      // power
		{"2^3^2", 512, false},  // right-associative power: 2^(3^2)
		{"-2^2", -4, false},    // unary binds tighter than ^ here: (-2)^2? -> unary applies to 2, then ^2 => 4; see below
		{"2*-3", -6, false},    // multiply by negative
		{"1/0", 0, true},       // division by zero
		{"", 0, true},          // empty
		{"2++", 0, true},       // invalid
		{"(2+3", 0, true},      // missing closing paren
		{"2+abc", 0, true},     // bad char
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
