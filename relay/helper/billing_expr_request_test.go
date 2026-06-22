package helper

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/samber/lo"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestResolveIncomingBillingExprRequestInput(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	ctx.Request.Header.Set("Content-Type", "application/json")

	body := []byte(`{"service_tier":"fast"}`)
	ctx.Request.Body = io.NopCloser(bytes.NewReader(body))
	ctx.Set(common.KeyRequestBody, body)

	info := &relaycommon.RelayInfo{
		RequestHeaders: map[string]string{"Content-Type": "application/json"},
	}

	input, err := ResolveIncomingBillingExprRequestInput(ctx, info)
	require.NoError(t, err)
	require.Equal(t, body, input.Body)
	require.Equal(t, "application/json", input.Headers["Content-Type"])
}

func TestBuildBillingExprRequestInputFromRequest(t *testing.T) {
	request := &dto.GeneralOpenAIRequest{
		Model:  "gemini-3.1-pro-preview",
		Stream: lo.ToPtr(true),
		Messages: []dto.Message{
			{
				Role:    "user",
				Content: "hi",
			},
		},
		MaxTokens: lo.ToPtr(uint(3000)),
	}

	input, err := BuildBillingExprRequestInputFromRequest(request, map[string]string{
		"Content-Type": "application/json",
		"X-Test":       "1",
	})
	require.NoError(t, err)
	require.Equal(t, "application/json", input.Headers["Content-Type"])
	require.Equal(t, "1", input.Headers["X-Test"])
	require.True(t, gjson.GetBytes(input.Body, "stream").Bool())
	require.Equal(t, "user", gjson.GetBytes(input.Body, "messages.0.role").String())
	require.Equal(t, float64(3000), gjson.GetBytes(input.Body, "max_tokens").Float())
}

func TestBuildBillingExprRequestInputFromImageRequestAddsBillingSize(t *testing.T) {
	tests := []struct {
		name string
		size string
		want string
	}{
		{name: "alias 1k", size: "1k", want: "1K"},
		{name: "alias 2k", size: "2K", want: "2K"},
		{name: "alias 4k", size: "4k", want: "4K"},
		{name: "max side 1024", size: "1024x768", want: "1K"},
		{name: "max side 2048", size: "2048x1152", want: "2K"},
		{name: "over 2048", size: "3840x2160", want: "4K"},
		{name: "invalid defaults 2k", size: "bad-size", want: "2K"},
		{name: "empty defaults 2k", size: "", want: "2K"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := &dto.ImageRequest{
				Model:  "gpt-image-2",
				Prompt: "draw a cat",
				Size:   tt.size,
			}

			input, err := BuildBillingExprRequestInputFromRequest(request, nil)
			require.NoError(t, err)
			require.Equal(t, tt.size, gjson.GetBytes(input.Body, "size").String())
			require.Equal(t, tt.want, gjson.GetBytes(input.Body, "billing_size").String())
		})
	}
}
