package main

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/joho/godotenv"
)

const (
	defaultTimeout = 60 * time.Minute
	awsAlgorithm   = "AWS4-HMAC-SHA256"
	r2Service      = "s3"
)

type config struct {
	PostgresDSN     string
	PgDumpPath      string
	DockerPath      string
	DockerContainer string
	PostgresUser    string
	PostgresDB      string
	OutputDir       string
	BackupDate      string
	Timeout         time.Duration
	R2AccountID     string
	R2AccessKeyID   string
	R2SecretKey     string
	R2Bucket        string
	R2ObjectPrefix  string
	R2PublicBaseURL string
	FeishuWebhook   string
	FeishuKeyword   string
	KeepLocal       bool
}

func main() {
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)
	_ = godotenv.Load("new-api-token-export.env")
	_ = godotenv.Load("new-api-postgres-backup.env")
	_ = godotenv.Load()

	cfg, err := loadConfig()
	if err != nil {
		log.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), cfg.Timeout)
	defer cancel()

	filePath, size, err := backupPostgres(ctx, cfg)
	if err != nil {
		log.Fatal(err)
	}
	if !cfg.KeepLocal {
		defer func() {
			if err := os.Remove(filePath); err != nil {
				log.Printf("remove local backup failed: %v", err)
			}
		}()
	}

	link, err := uploadToR2(ctx, cfg, filePath)
	if err != nil {
		log.Fatal(err)
	}

	if cfg.FeishuWebhook != "" {
		if err := notifyFeishu(ctx, cfg, link, size); err != nil {
			log.Fatal(err)
		}
	}

	log.Printf("postgres backup finished: size=%d link=%s", size, link)
}

func loadConfig() (config, error) {
	cfg := config{
		PostgresDSN:     strings.TrimSpace(firstNonEmpty(os.Getenv("POSTGRES_DSN"), os.Getenv("SQL_DSN"))),
		PgDumpPath:      strings.TrimSpace(getEnv("PG_DUMP_PATH", "pg_dump")),
		DockerPath:      strings.TrimSpace(getEnv("DOCKER_PATH", "docker")),
		DockerContainer: strings.TrimSpace(getEnv("POSTGRES_BACKUP_CONTAINER", "postgres-api")),
		PostgresUser:    strings.TrimSpace(getEnv("POSTGRES_BACKUP_USER", "root")),
		PostgresDB:      strings.TrimSpace(getEnv("POSTGRES_BACKUP_DB", "new-api")),
		OutputDir:       strings.TrimSpace(getEnv("POSTGRES_BACKUP_OUTPUT_DIR", "/backup")),
		BackupDate:      strings.TrimSpace(os.Getenv("POSTGRES_BACKUP_DATE")),
		Timeout:         time.Duration(getEnvInt("POSTGRES_BACKUP_TIMEOUT_MINUTES", int(defaultTimeout/time.Minute))) * time.Minute,
		R2AccountID:     strings.TrimSpace(os.Getenv("R2_ACCOUNT_ID")),
		R2AccessKeyID:   strings.TrimSpace(os.Getenv("R2_ACCESS_KEY_ID")),
		R2SecretKey:     strings.TrimSpace(os.Getenv("R2_SECRET_ACCESS_KEY")),
		R2Bucket:        strings.TrimSpace(firstNonEmpty(os.Getenv("POSTGRES_BACKUP_R2_BUCKET"), os.Getenv("R2_BACKUP_BUCKET"), os.Getenv("R2_BUCKET"))),
		R2ObjectPrefix:  strings.Trim(strings.TrimSpace(getEnv("POSTGRES_BACKUP_R2_OBJECT_PREFIX", "daily/postgres")), "/"),
		R2PublicBaseURL: strings.TrimRight(strings.TrimSpace(os.Getenv("R2_PUBLIC_BASE_URL")), "/"),
		FeishuWebhook:   strings.TrimSpace(os.Getenv("FEISHU_WEBHOOK_URL")),
		FeishuKeyword:   strings.TrimSpace(os.Getenv("FEISHU_KEYWORD")),
		KeepLocal:       getEnvBool("POSTGRES_BACKUP_KEEP_LOCAL", false),
	}

	if cfg.BackupDate == "" {
		cfg.BackupDate = time.Now().Format("2006-01-02")
	}
	if cfg.PostgresDSN != "" && !strings.HasPrefix(cfg.PostgresDSN, "postgres://") && !strings.HasPrefix(cfg.PostgresDSN, "postgresql://") {
		return cfg, errors.New("POSTGRES_DSN or SQL_DSN must be a PostgreSQL connection URL")
	}
	if cfg.PostgresDSN == "" && (cfg.DockerContainer == "" || cfg.PostgresUser == "" || cfg.PostgresDB == "") {
		return cfg, errors.New("POSTGRES_BACKUP_CONTAINER, POSTGRES_BACKUP_USER, and POSTGRES_BACKUP_DB are required for docker backup")
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = defaultTimeout
	}
	if cfg.r2PartiallyConfigured() || !cfg.r2Enabled() {
		return cfg, errors.New("R2 upload requires R2_ACCOUNT_ID, R2_ACCESS_KEY_ID, R2_SECRET_ACCESS_KEY, and R2_BUCKET")
	}
	return cfg, nil
}

func (cfg config) r2Enabled() bool {
	return cfg.R2AccountID != "" && cfg.R2AccessKeyID != "" && cfg.R2SecretKey != "" && cfg.R2Bucket != ""
}

func (cfg config) r2PartiallyConfigured() bool {
	values := []string{cfg.R2AccountID, cfg.R2AccessKeyID, cfg.R2SecretKey, cfg.R2Bucket}
	set := 0
	for _, value := range values {
		if value != "" {
			set++
		}
	}
	return set > 0 && set < len(values)
}

func backupPostgres(ctx context.Context, cfg config) (string, int64, error) {
	if err := os.MkdirAll(cfg.OutputDir, 0o755); err != nil {
		return "", 0, err
	}

	filePath := filepath.Join(cfg.OutputDir, fmt.Sprintf("app-%s.dump", cfg.BackupDate))
	file, err := os.Create(filePath)
	if err != nil {
		return "", 0, err
	}
	defer file.Close()

	cmd := pgDumpCommand(ctx, cfg)
	cmd.Stdout = file
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		_ = file.Close()
		_ = os.Remove(filePath)
		message := strings.TrimSpace(stderr.String())
		if message != "" {
			return "", 0, fmt.Errorf("pg_dump failed: %w: %s", err, message)
		}
		return "", 0, fmt.Errorf("pg_dump failed: %w", err)
	}
	if err := file.Close(); err != nil {
		return "", 0, err
	}

	stat, err := os.Stat(filePath)
	if err != nil {
		return "", 0, err
	}
	return filePath, stat.Size(), nil
}

func pgDumpCommand(ctx context.Context, cfg config) *exec.Cmd {
	if cfg.PostgresDSN != "" {
		return exec.CommandContext(ctx, cfg.PgDumpPath, "--no-owner", "--no-privileges", "--clean", "--if-exists", "--format=custom", cfg.PostgresDSN)
	}
	return exec.CommandContext(ctx, cfg.DockerPath, "exec", "-i", cfg.DockerContainer, "pg_dump", "-U", cfg.PostgresUser, "-d", cfg.PostgresDB, "-Fc")
}

func uploadToR2(ctx context.Context, cfg config, filePath string) (string, error) {
	payloadHash, err := fileSHA256Hex(filePath)
	if err != nil {
		return "", err
	}
	body, err := os.Open(filePath)
	if err != nil {
		return "", err
	}
	defer body.Close()
	stat, err := body.Stat()
	if err != nil {
		return "", err
	}
	objectKey := filepath.Base(filePath)
	if cfg.R2ObjectPrefix != "" {
		objectKey = cfg.R2ObjectPrefix + "/" + objectKey
	}
	escapedKey := escapeObjectKey(objectKey)
	endpoint := fmt.Sprintf("https://%s.r2.cloudflarestorage.com/%s/%s", cfg.R2AccountID, cfg.R2Bucket, escapedKey)

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, endpoint, body)
	if err != nil {
		return "", err
	}
	req.ContentLength = stat.Size()
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("X-Amz-Content-Sha256", payloadHash)
	signR2Request(req, cfg, payloadHash)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		message, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return "", fmt.Errorf("R2 upload failed: status=%d body=%s", resp.StatusCode, string(message))
	}

	if cfg.R2PublicBaseURL != "" {
		return cfg.R2PublicBaseURL + "/" + escapedKey, nil
	}
	return endpoint, nil
}

func signR2Request(req *http.Request, cfg config, payloadHash string) {
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
		awsAlgorithm,
		amzDate,
		credentialScope,
		sha256Hex([]byte(canonicalRequest)),
	}, "\n")
	signature := hex.EncodeToString(hmacSHA256(signingKey(cfg.R2SecretKey, dateStamp), []byte(stringToSign)))
	auth := fmt.Sprintf("%s Credential=%s/%s, SignedHeaders=%s, Signature=%s", awsAlgorithm, cfg.R2AccessKeyID, credentialScope, signedHeaders, signature)
	req.Header.Set("Authorization", auth)
}

func signingKey(secret, dateStamp string) []byte {
	kDate := hmacSHA256([]byte("AWS4"+secret), []byte(dateStamp))
	kRegion := hmacSHA256(kDate, []byte("auto"))
	kService := hmacSHA256(kRegion, []byte(r2Service))
	return hmacSHA256(kService, []byte("aws4_request"))
}

func notifyFeishu(ctx context.Context, cfg config, link string, size int64) error {
	message := fmt.Sprintf("PostgreSQL 备份完成\n日期：%s\n大小：%s\n链接：%s", cfg.BackupDate, humanSize(size), link)
	if cfg.FeishuKeyword != "" {
		message = cfg.FeishuKeyword + "\n" + message
	}
	payload, err := common.Marshal(map[string]any{
		"msg_type": "text",
		"content": map[string]string{
			"text": message,
		},
	})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.FeishuWebhook, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		message, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("feishu notify failed: status=%d body=%s", resp.StatusCode, string(message))
	}
	return nil
}

func escapeObjectKey(key string) string {
	parts := strings.Split(key, "/")
	for i := range parts {
		parts[i] = url.PathEscape(parts[i])
	}
	return strings.Join(parts, "/")
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func fileSHA256Hex(filePath string) (string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func hmacSHA256(key []byte, data []byte) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write(data)
	return mac.Sum(nil)
}

func humanSize(size int64) string {
	units := []string{"B", "KiB", "MiB", "GiB", "TiB"}
	value := float64(size)
	unit := 0
	for value >= 1024 && unit < len(units)-1 {
		value /= 1024
		unit++
	}
	if unit == 0 {
		return fmt.Sprintf("%d %s", size, units[unit])
	}
	return fmt.Sprintf("%.2f %s", value, units[unit])
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func getEnv(name string, fallback string) string {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	return value
}

func getEnvInt(name string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func getEnvBool(name string, fallback bool) bool {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return fallback
	}
	return parsed
}
