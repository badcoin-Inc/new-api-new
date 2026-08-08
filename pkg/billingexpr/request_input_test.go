package billingexpr

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTrimRequestInputForStorageKeepsReferencedSafeHeader(t *testing.T) {
	input := RequestInput{
		Headers: map[string]string{
			"Authorization":  "Bearer secret",
			"Cookie":         "session=secret",
			"Anthropic-Beta": "fast-mode-2026-02-01",
			"User-Agent":     "client",
		},
		Body: []byte(`{"size":"1024x1024"}`),
	}

	trimmed := TrimRequestInputForStorage(`p + c * (has(header("anthropic-beta"), "fast-mode") ? 2 : 1)`, input)

	require.Equal(t, []byte(`{"size":"1024x1024"}`), trimmed.Body)
	assert.Equal(t, map[string]string{"Anthropic-Beta": "fast-mode-2026-02-01"}, trimmed.Headers)
}

func TestTrimRequestInputForStorageDropsHeadersWhenUnused(t *testing.T) {
	input := RequestInput{Headers: map[string]string{"User-Agent": "client"}}

	trimmed := TrimRequestInputForStorage("p * 2 + c * 8", input)

	assert.Empty(t, trimmed.Headers)
}
