package service

import (
	"context"
	"encoding/base64"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestStoreGenerationJobImageResultsKeepsUpstreamURL(t *testing.T) {
	body := []byte(`{"created":1710000000,"data":[{"url":"https://upstream.example/image.png","revised_prompt":"cat"}],"usage":{"total_tokens":3}}`)
	uploadCalled := false

	rewritten, err := storeGenerationJobImageResults(context.Background(), body, R2UploadConfig{ExpireSeconds: defaultR2UploadExpireSeconds}, func(context.Context, R2UploadConfig, []byte, string, string) (string, error) {
		uploadCalled = true
		return "", nil
	})

	require.NoError(t, err)
	assert.False(t, uploadCalled)
	assert.Equal(t, "https://upstream.example/image.png", gjson.GetBytes(rewritten, "data.0.url").String())
	assert.False(t, gjson.GetBytes(rewritten, "data.0.b64_json").Exists())
	assert.False(t, gjson.GetBytes(rewritten, "data.0.revised_prompt").Exists())
	assert.False(t, gjson.GetBytes(rewritten, "usage").Exists())
}

func TestStoreGenerationJobImageResultsPrefersBase64WhenURLAlsoPresent(t *testing.T) {
	pngBase64 := "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAIAAACQd1PeAAAADElEQVQI12P4//8/AAX+Av7czFnnAAAAAElFTkSuQmCC"
	body := []byte(`{"data":[{"url":"https://upstream.example/content-token","b64_json":"` + pngBase64 + `"}]}`)

	rewritten, err := storeGenerationJobImageResults(context.Background(), body, R2UploadConfig{ExpireSeconds: 0}, func(_ context.Context, _ R2UploadConfig, data []byte, filename string, contentType string) (string, error) {
		require.NotEmpty(t, data)
		require.Equal(t, "result.png", filename)
		require.Equal(t, "image/png", contentType)
		return "https://cdn.example/generation-jobs/image.png", nil
	})

	require.NoError(t, err)
	assert.Equal(t, "https://cdn.example/generation-jobs/image.png", gjson.GetBytes(rewritten, "data.0.url").String())
	assert.False(t, gjson.GetBytes(rewritten, "data.0.b64_json").Exists())
}

func TestStoreGenerationJobImageResultsUploadsBase64ToR2(t *testing.T) {
	pngBase64 := "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAIAAACQd1PeAAAADElEQVQI12P4//8/AAX+Av7czFnnAAAAAElFTkSuQmCC"
	body := []byte(`{"created":1710000000,"data":[{"b64_json":"` + pngBase64 + `","revised_prompt":"cat"}]}`)
	wantImage, err := base64.StdEncoding.DecodeString(pngBase64)
	require.NoError(t, err)
	before := time.Now().Unix()

	rewritten, err := storeGenerationJobImageResults(context.Background(), body, R2UploadConfig{ExpireSeconds: defaultR2UploadExpireSeconds}, func(_ context.Context, _ R2UploadConfig, data []byte, filename string, contentType string) (string, error) {
		assert.Equal(t, wantImage, data)
		assert.Equal(t, "result.png", filename)
		assert.Equal(t, "image/png", contentType)
		return "https://cdn.example/generation-jobs/image.png", nil
	})

	require.NoError(t, err)
	assert.Equal(t, "https://cdn.example/generation-jobs/image.png", gjson.GetBytes(rewritten, "data.0.url").String())
	assert.False(t, gjson.GetBytes(rewritten, "data.0.b64_json").Exists())
	assert.Equal(t, "image/png", gjson.GetBytes(rewritten, "data.0.mime_type").String())
	expiresAt := gjson.GetBytes(rewritten, "data.0.expires_at").Int()
	assert.GreaterOrEqual(t, expiresAt, before+int64(defaultR2UploadExpireSeconds))
	assert.LessOrEqual(t, expiresAt, time.Now().Unix()+int64(defaultR2UploadExpireSeconds))
}

func TestStoreGenerationJobImageResultsPreservesBase64WhenUploadFails(t *testing.T) {
	pngBase64 := "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAIAAACQd1PeAAAADElEQVQI12P4//8/AAX+Av7czFnnAAAAAElFTkSuQmCC"
	body := []byte(`{"data":[{"b64_json":"` + pngBase64 + `"}]}`)

	rewritten, err := storeGenerationJobImageResults(context.Background(), body, R2UploadConfig{ExpireSeconds: defaultR2UploadExpireSeconds}, func(context.Context, R2UploadConfig, []byte, string, string) (string, error) {
		return "", errors.New("R2 unavailable")
	})

	require.Error(t, err)
	assert.Equal(t, pngBase64, gjson.GetBytes(rewritten, "data.0.b64_json").String())
	assert.False(t, gjson.GetBytes(rewritten, "data.0.url").Exists())
}

func TestLoadR2UploadConfigDefaultsToSevenDays(t *testing.T) {
	t.Setenv("R2_UPLOAD_EXPIRE_SECONDS", "")

	assert.Equal(t, defaultR2UploadExpireSeconds, LoadR2UploadConfig().ExpireSeconds)
}
