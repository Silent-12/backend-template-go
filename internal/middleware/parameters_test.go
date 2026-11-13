package middleware

import (
	"encoding/json"
	"math"
	"reflect"
	"testing"
)

func TestScalarTypesRejectOverflowAndCoercion(t *testing.T) {
	for _, tc := range []struct {
		name          string
		value, target any
		valid         bool
	}{
		{"signed upper bound", json.Number("127"), int8(0), true},
		{"signed overflow", 128, int8(0), false},
		{"signed underflow", -129, int8(0), false},
		{"unsigned overflow into signed", uint64(math.MaxUint64), int64(0), false},
		{"fraction into signed", json.Number("1.5"), int(0), false},
		{"unsigned upper bound", json.Number("18446744073709551615"), uint64(0), true},
		{"unsigned overflow", json.Number("18446744073709551616"), uint64(0), false},
		{"negative into unsigned", "-1", uint8(0), false},
		{"unsigned width overflow", "256", uint8(0), false},
		{"array into unsigned", []int{1}, uint(0), false},
		{"finite float", json.Number("1.25"), float32(0), true},
		{"float overflow", "1e100", float32(0), false},
		{"not a number", "NaN", float64(0), false},
		{"infinite number", math.Inf(1), float64(0), false},
		{"object into float", map[string]any{}, float64(0), false},
		{"JSON number into string", json.Number("42"), "", false},
		{"boolean string", "false", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := validScalar(tc.value, reflect.TypeOf(tc.target)); got != tc.valid {
				t.Fatalf("validScalar = %v, want %v", got, tc.valid)
			}
		})
	}
}
