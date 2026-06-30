package environment

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRefs(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  []string
	}{
		{"empty", "", nil},
		{"no refs", "huggingface.co/unsloth/model:Q3_K_M", nil},
		{"js form", "${env.NEMOTRON3_MODEL}", []string{"NEMOTRON3_MODEL"}},
		{"shell form", "${DMR_BASE_URL}", []string{"DMR_BASE_URL"}},
		{"bare form", "$MODEL", []string{"MODEL"}},
		{"mixed and embedded", "http://${env.HOST}:${PORT}/v1", []string{"HOST", "PORT"}},
		{"deduplicated", "${env.X}-${X}", []string{"X"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, Refs(tt.value))
		})
	}
}
