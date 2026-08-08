package controller

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerationJobPriceSnapshotOmitsBillingRequestInput(t *testing.T) {
	priceData := types.PriceData{
		TieredBillingSnapshot: &billingexpr.BillingSnapshot{
			ExprString: `p + c * (has(header("anthropic-beta"), "fast-mode") ? 2 : 1)`,
		},
		BillingRequestInput: &billingexpr.RequestInput{
			Headers: map[string]string{
				"Authorization":  "Bearer secret",
				"Cookie":         "session=secret",
				"Anthropic-Beta": "fast-mode-2026-02-01",
				"User-Agent":     "client",
			},
			Body: []byte(`{"prompt":"cat"}`),
		},
	}

	stored, err := generationJobPriceSnapshot(priceData)
	require.NoError(t, err)
	assert.NotContains(t, string(stored), "BillingRequestInput")
	assert.NotContains(t, string(stored), "Bearer secret")
	assert.NotContains(t, string(stored), "session=secret")

	var persisted types.PriceData
	require.NoError(t, common.Unmarshal(stored, &persisted))
	require.NotNil(t, persisted.TieredBillingSnapshot)
	assert.Equal(t, map[string]string{"Anthropic-Beta": "fast-mode-2026-02-01"}, persisted.TieredBillingSnapshot.RequestHeaders)
}

func TestRestoreGenerationJobBillingRequestInputRebuildsBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/v1/images/generations", strings.NewReader(`{"model":"gpt-image-2","prompt":"cat","size":"1024x1024","n":1}`))
	c.Request.Header.Set("Content-Type", gin.MIMEJSON)
	defer common.CleanupBodyStorage(c)

	imageReq := &dto.ImageRequest{Model: "gpt-image-2", Prompt: "cat", Size: "1024x1024"}
	info := relaycommon.GenRelayInfoImage(c, imageReq)
	info.TieredBillingSnapshot = &billingexpr.BillingSnapshot{
		RequestHeaders: map[string]string{"Anthropic-Beta": "fast-mode-2026-02-01"},
	}

	require.NoError(t, restoreGenerationJobBillingRequestInput(c, info))
	require.NotNil(t, info.BillingRequestInput)
	assert.Contains(t, string(info.BillingRequestInput.Body), `"billing_size":"1K"`)
	assert.Equal(t, "fast-mode-2026-02-01", info.BillingRequestInput.Headers["Anthropic-Beta"])
}
