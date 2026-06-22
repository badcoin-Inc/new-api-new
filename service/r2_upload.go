package service

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
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
)

const r2Service = "s3"

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
		ExpireSeconds: common.GetEnvOrDefault("R2_UPLOAD_EXPIRE_SECONDS", 604800),
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
	if !cfg.Enabled() {
		return "", errors.New("R2 upload requires R2_ACCOUNT_ID, R2_ACCESS_KEY_ID, R2_SECRET_ACCESS_KEY, R2_BUCKET, and R2_PUBLIC_BASE_URL")
	}
	if contentType == "" {
		contentType = http.DetectContentType(data)
	}
	objectKey := generationJobObjectKey(cfg.ObjectPrefix, filename)
	return uploadBytesToR2(ctx, cfg, objectKey, contentType, data)
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
