package identifier_test

import (
	"testing"

	"github.com/datum-cloud/galactic-operator/internal/identifier"
)

func TestNewFromInteger(t *testing.T) {
	tests := []struct {
		name           string
		value          uint64
		wantIdentifier string
		wantError      bool
	}{
		{"InvalidSpecialMin", 0, "", true},
		{"InvalidSpecialMax", identifier.MaxIdentifier, "", true},
		{"ValidMin", identifier.MinValidIdentifier, "000000000001", false},
		{"ValidMax", identifier.MaxValidIdentifier, "fffffffffffe", false},
		{"Valid", 12345, "000000003039", false},
		{"InvalidMax", identifier.MaxIdentifier + 1, "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := identifier.NewFromInteger(tt.value)
			if (err != nil) != tt.wantError {
				t.Errorf("NewFromInteger() error = %v, wantError = %v", err, tt.wantError)
			}
			if got != tt.wantIdentifier {
				t.Errorf("NewFromInteger() got = %v, want = %v", got, tt.wantIdentifier)
			}
		})
	}
}
