package bft

import (
	"bytes"
	"testing"
)

func TestExpandMainchainMVBAInput(t *testing.T) {
	tests := []struct {
		name       string
		simulatedM int
		input      string
		want       string
	}{
		{
			name:       "use one copy when simulated m equals two",
			simulatedM: 2,
			input:      "1001",
			want:       "1001",
		},
		{
			name:       "repeat input simulated m minus one times",
			simulatedM: 4,
			input:      "1001",
			want:       "100110011001",
		},
		{
			name:       "clamp invalid simulated m to one copy",
			simulatedM: 1,
			input:      "1001",
			want:       "1001",
		},
		{
			name:       "preserve empty input",
			simulatedM: 5,
			input:      "",
			want:       "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := expandMainchainMVBAInput([]byte(tt.input), tt.simulatedM)
			if !bytes.Equal(got, []byte(tt.want)) {
				t.Fatalf("expandMainchainMVBAInput(%q, %d) = %q, want %q", tt.input, tt.simulatedM, string(got), tt.want)
			}
		})
	}
}
