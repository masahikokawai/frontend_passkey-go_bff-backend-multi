package v1

import "testing"

func TestSecureTokenEqual(t *testing.T) {
	tests := []struct {
		name string
		got  string
		want string
		eq   bool
	}{
		{"完全一致", "secret-123", "secret-123", true},
		{"不一致(同じ長さ)", "secret-123", "secret-456", false},
		{"不一致(長さ違い、短い)", "secret", "secret-123", false},
		{"不一致(長さ違い、長い)", "secret-123-extra", "secret-123", false},
		{"両方空", "", "", true},
		{"片方空", "", "secret-123", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := secureTokenEqual(tt.got, tt.want); got != tt.eq {
				t.Errorf("secureTokenEqual(%q, %q) = %v, want %v", tt.got, tt.want, got, tt.eq)
			}
		})
	}
}
