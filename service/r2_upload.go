package service

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

const (
	r2Service                    = "s3"
	defaultR2UploadExpireSeconds = 7 * 24 * 60 * 60
)

type R2UploadConfig struct {
	AccountID     string
	AccessKeyID   string
	SecretKey     string
	Bucket        string
	ObjectPrefix  string
	PublicBaseURL string
	ExpireSeconds int
}

func LoadR2UploadConfig() R2UploadConfig {
	return R2UploadConfig{
		AccountID:     strings.TrimSpace(os.Getenv("R2_ACCOUNT_ID")),
		AccessKeyID:   strings.TrimSpace(os.Getenv("R2_ACCESS_KEY_ID")),
		SecretKey:     strings.TrimSpace(os.Getenv("R2_SECRET_ACCESS_KEY")),
		Bucket:        strings.TrimSpace(os.Getenv("R2_BUCKET")),
		ObjectPrefix:  strings.Trim(strings.TrimSpace(common.GetEnvOrDefaultString("R2_OBJECT_PREFIX", "generation-jobs")), "/"),
		PublicBaseURL: strings.TrimRight(strings.TrimSpace(os.Getenv("R2_PUBLIC_BASE_URL")), "/"),
		ExpireSeconds: common.GetEnvOrDefault("R2_UPLOAD_EXPIRE_SECONDS", defaultR2UploadExpireSeconds),
	}
}

func R2UploadInputExpiresAt() int64 {
	seconds := LoadR2UploadConfig().ExpireSeconds
	if seconds <= 0 {
		return 0
	}
	return time.Now().Unix() + int64(seconds)
}

func (c R2UploadConfig) Enabled() bool {
	return c.AccountID != "" && c.AccessKeyID != "" && c.SecretKey != "" && c.Bucket != "" && c.PublicBaseURL != ""
}

func UploadGenerationJobObject(ctx context.Context, data []byte, filename string, contentType string) (string, error) {
	cfg := LoadR2UploadConfig()
	return uploadGenerationJobObject(ctx, cfg, data, filename, contentType)
}

func uploadGenerationJobObject(ctx context.Context, cfg R2UploadConfig, data []byte, filename string, contentType string) (string, error) {
	if !cfg.Enabled() {
		return "", errors.New("R2 upload requires R2_ACCOUNT_ID, R2_ACCESS_KEY_ID, R2_SECRET_ACCESS_KEY, R2_BUCKET, and R2_PUBLIC_BASE_URL")
	}
	if contentType == "" {
		contentType = http.DetectContentType(data)
	}
	objectKey := generationJobObjectKey(cfg.ObjectPrefix, filename)
	return uploadBytesToR2(ctx, cfg, objectKey, contentType, data)
}

type generationJobImageUploader func(context.Context, R2UploadConfig, []byte, string, string) (string, error)

// StoreGenerationJobImageResults keeps upstream URLs unchanged and replaces
// base64 image results with R2-backed URLs before a generation job is stored.
func StoreGenerationJobImageResults(ctx context.Context, responseBody []byte) ([]byte, error) {
	return storeGenerationJobImageResults(ctx, responseBody, LoadR2UploadConfig(), uploadGenerationJobObject)
}

func storeGenerationJobImageResults(ctx context.Context, responseBody []byte, cfg R2UploadConfig, upload generationJobImageUploader) ([]byte, error) {
	if !gjson.ValidBytes(responseBody) {
		return responseBody, errors.New("generation job image response is not valid JSON")
	}
	data := gjson.GetBytes(responseBody, "data")
	if !data.IsArray() {
		return responseBody, nil
	}

	rewritten := responseBody
	var resultErr error
	expiresAt := int64(0)
	if cfg.ExpireSeconds > 0 {
		expiresAt = time.Now().Unix() + int64(cfg.ExpireSeconds)
	}

	data.ForEach(func(key, value gjson.Result) bool {
		index := key.Int()
		path := fmt.Sprintf("data.%d", index)
		rawURL := strings.TrimSpace(value.Get("url").String())
		base64Result := strings.TrimSpace(value.Get("b64_json").String())
		isDataURL := strings.HasPrefix(strings.ToLower(rawURL), "data:")

		if base64Result == "" && isDataURL {
			base64Result = rawURL
		}
		if base64Result == "" {
			return true
		}

		imageData, contentType, err := decodeGenerationJobImageResult(base64Result)
		if err != nil {
			if rawURL != "" && !isDataURL {
				updated, deleteErr := sjson.DeleteBytes(rewritten, path+".b64_json")
				if deleteErr != nil {
					resultErr = errors.Join(resultErr, fmt.Errorf("remove image result %d base64: %w", index, deleteErr))
					return true
				}
				rewritten = updated
				return true
			}
			resultErr = errors.Join(resultErr, fmt.Errorf("decode image result %d: %w", index, err))
			return true
		}
		storedURL, err := upload(ctx, cfg, imageData, generationJobImageResultFilename(contentType), contentType)
		if err != nil {
			if rawURL != "" && !isDataURL {
				updated, deleteErr := sjson.DeleteBytes(rewritten, path+".b64_json")
				if deleteErr != nil {
					resultErr = errors.Join(resultErr, fmt.Errorf("remove image result %d base64: %w", index, deleteErr))
					return true
				}
				rewritten = updated
				return true
			}
			resultErr = errors.Join(resultErr, fmt.Errorf("upload image result %d: %w", index, err))
			return true
		}

		updated, err := sjson.SetBytes(rewritten, path+".url", storedURL)
		if err != nil {
			resultErr = errors.Join(resultErr, fmt.Errorf("set image result %d url: %w", index, err))
			return true
		}
		rewritten = updated
		updated, err = sjson.DeleteBytes(rewritten, path+".b64_json")
		if err != nil {
			resultErr = errors.Join(resultErr, fmt.Errorf("remove image result %d base64: %w", index, err))
			return true
		}
		rewritten = updated
		if expiresAt > 0 {
			updated, err = sjson.SetBytes(rewritten, path+".expires_at", expiresAt)
			if err != nil {
				resultErr = errors.Join(resultErr, fmt.Errorf("set image result %d expiration: %w", index, err))
				return true
			}
			rewritten = updated
		}
		updated, err = sjson.SetBytes(rewritten, path+".mime_type", contentType)
		if err != nil {
			resultErr = errors.Join(resultErr, fmt.Errorf("set image result %d mime type: %w", index, err))
			return true
		}
		rewritten = updated
		return true
	})

	if resultErr == nil {
		// The worker only needs downloadable image metadata after R2 upload;
		// omit provider usage and prompt fields from the persisted result.
		compacted, err := compactGenerationJobImageResponse(rewritten)
		if err != nil {
			return rewritten, err
		}
		rewritten = compacted
	}
	return rewritten, resultErr
}

func compactGenerationJobImageResponse(responseBody []byte) ([]byte, error) {
	data := gjson.GetBytes(responseBody, "data")
	if !data.IsArray() {
		return responseBody, nil
	}
	images := make([]map[string]any, 0)
	data.ForEach(func(_ gjson.Result, value gjson.Result) bool {
		image := make(map[string]any)
		if url := strings.TrimSpace(value.Get("url").String()); url != "" {
			image["url"] = url
		}
		if expiresAt := value.Get("expires_at"); expiresAt.Exists() {
			image["expires_at"] = expiresAt.Int()
		}
		if mimeType := strings.TrimSpace(value.Get("mime_type").String()); mimeType != "" {
			image["mime_type"] = mimeType
		}
		images = append(images, image)
		return true
	})
	return common.Marshal(map[string]any{"data": images})
}

func decodeGenerationJobImageResult(raw string) ([]byte, string, error) {
	raw = strings.TrimSpace(raw)
	contentType := ""
	if strings.HasPrefix(strings.ToLower(raw), "data:") {
		comma := strings.Index(raw, ",")
		if comma == -1 {
			return nil, "", errors.New("invalid image data URL")
		}
		metadata := strings.Split(raw[len("data:"):comma], ";")
		if len(metadata) > 0 {
			contentType = strings.TrimSpace(metadata[0])
		}
		isBase64 := false
		for _, part := range metadata[1:] {
			if strings.EqualFold(strings.TrimSpace(part), "base64") {
				isBase64 = true
				break
			}
		}
		if !isBase64 {
			return nil, "", errors.New("image data URL is not base64 encoded")
		}
		raw = raw[comma+1:]
	}
	if raw == "" {
		return nil, "", errors.New("image base64 result is empty")
	}

	decoded, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		decoded, err = base64.RawStdEncoding.DecodeString(raw)
	}
	if err != nil {
		return nil, "", err
	}
	if len(decoded) == 0 {
		return nil, "", errors.New("decoded image result is empty")
	}
	if contentType == "" || strings.EqualFold(contentType, "application/octet-stream") {
		contentType = http.DetectContentType(decoded)
	}
	contentType = strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0]))
	if !strings.HasPrefix(contentType, "image/") {
		return nil, "", fmt.Errorf("invalid image content type: %s", contentType)
	}
	return decoded, contentType, nil
}

func generationJobImageResultFilename(contentType string) string {
	extension := ".bin"
	switch strings.ToLower(strings.TrimSpace(contentType)) {
	case "image/png":
		extension = ".png"
	case "image/jpeg", "image/jpg":
		extension = ".jpg"
	case "image/webp":
		extension = ".webp"
	case "image/gif":
		extension = ".gif"
	case "image/avif":
		extension = ".avif"
	case "image/heic":
		extension = ".heic"
	case "image/heif":
		extension = ".heif"
	}
	return "result" + extension
}

func generationJobObjectKey(prefix string, filename string) string {
	ext := strings.ToLower(filepath.Ext(filename))
	if ext == "" || len(ext) > 10 {
		ext = ".bin"
	}
	key, _ := common.GenerateRandomCharsKey(32)
	objectKey := fmt.Sprintf("%s/%s%s", time.Now().UTC().Format("2006/01/02"), key, ext)
	if prefix != "" {
		objectKey = prefix + "/" + objectKey
	}
	return objectKey
}

func uploadBytesToR2(ctx context.Context, cfg R2UploadConfig, objectKey string, contentType string, data []byte) (string, error) {
	payloadHash := sha256HexBytes(data)
	escapedKey := escapeR2ObjectKey(objectKey)
	endpoint := fmt.Sprintf("https://%s.r2.cloudflarestorage.com/%s/%s", cfg.AccountID, cfg.Bucket, escapedKey)
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, endpoint, bytes.NewReader(data))
	if err != nil {
		return "", err
	}
	req.ContentLength = int64(len(data))
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("X-Amz-Content-Sha256", payloadHash)
	signR2UploadRequest(req, cfg, payloadHash)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		message, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return "", fmt.Errorf("R2 upload failed: status=%d body=%s", resp.StatusCode, string(message))
	}
	return cfg.PublicBaseURL + "/" + escapedKey, nil
}

func signR2UploadRequest(req *http.Request, cfg R2UploadConfig, payloadHash string) {
	now := time.Now().UTC()
	amzDate := now.Format("20060102T150405Z")
	dateStamp := now.Format("20060102")
	credentialScope := dateStamp + "/auto/" + r2Service + "/aws4_request"

	req.Header.Set("Host", req.URL.Host)
	req.Header.Set("X-Amz-Date", amzDate)
	signedHeaders := "content-type;host;x-amz-content-sha256;x-amz-date"
	canonicalHeaders := "content-type:" + req.Header.Get("Content-Type") + "\n" +
		"host:" + req.URL.Host + "\n" +
		"x-amz-content-sha256:" + payloadHash + "\n" +
		"x-amz-date:" + amzDate + "\n"
	canonicalRequest := strings.Join([]string{
		req.Method,
		req.URL.EscapedPath(),
		"",
		canonicalHeaders,
		signedHeaders,
		payloadHash,
	}, "\n")
	stringToSign := strings.Join([]string{
		"AWS4-HMAC-SHA256",
		amzDate,
		credentialScope,
		sha256HexBytes([]byte(canonicalRequest)),
	}, "\n")
	signature := hex.EncodeToString(hmacSHA256Bytes(r2SigningKey(cfg.SecretKey, dateStamp), []byte(stringToSign)))
	auth := fmt.Sprintf("AWS4-HMAC-SHA256 Credential=%s/%s, SignedHeaders=%s, Signature=%s", cfg.AccessKeyID, credentialScope, signedHeaders, signature)
	req.Header.Set("Authorization", auth)
}

func r2SigningKey(secret, dateStamp string) []byte {
	kDate := hmacSHA256Bytes([]byte("AWS4"+secret), []byte(dateStamp))
	kRegion := hmacSHA256Bytes(kDate, []byte("auto"))
	kService := hmacSHA256Bytes(kRegion, []byte(r2Service))
	return hmacSHA256Bytes(kService, []byte("aws4_request"))
}

func escapeR2ObjectKey(key string) string {
	parts := strings.Split(key, "/")
	for i := range parts {
		parts[i] = url.PathEscape(parts[i])
	}
	return strings.Join(parts, "/")
}

func sha256HexBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func hmacSHA256Bytes(key []byte, data []byte) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write(data)
	return mac.Sum(nil)
}
