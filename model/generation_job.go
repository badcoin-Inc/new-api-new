package model

import (
	"encoding/json"
	"time"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

type GenerationJobStatus string

const (
	GenerationJobStatusQueued    GenerationJobStatus = "queued"
	GenerationJobStatusRunning   GenerationJobStatus = "running"
	GenerationJobStatusSucceeded GenerationJobStatus = "succeeded"
	GenerationJobStatusFailed    GenerationJobStatus = "failed"
	GenerationJobStatusCancelled GenerationJobStatus = "cancelled"
)

type GenerationJob struct {
	ID                 int64               `json:"id" gorm:"primary_key;AUTO_INCREMENT"`
	JobID              string              `json:"job_id" gorm:"type:varchar(191);uniqueIndex"`
	CreatedAt          int64               `json:"created_at" gorm:"index"`
	UpdatedAt          int64               `json:"updated_at"`
	UserId             int                 `json:"user_id" gorm:"index"`
	Group              string              `json:"group" gorm:"type:varchar(50)"`
	ChannelId          int                 `json:"channel_id" gorm:"index"`
	TokenId            int                 `json:"token_id" gorm:"index"`
	Model              string              `json:"model" gorm:"type:varchar(191);index"`
	Path               string              `json:"path" gorm:"type:varchar(191)"`
	ImageCount         int                 `json:"image_count"`
	Status             GenerationJobStatus `json:"status" gorm:"type:varchar(20);index"`
	Quota              int                 `json:"quota"`
	PreConsumedQuota   int                 `json:"pre_consumed_quota"`
	BillingSource      string              `json:"billing_source" gorm:"type:varchar(32)"`
	SubscriptionId     int                 `json:"subscription_id"`
	RetryCount         int                 `json:"retry_count"`
	NextRetryAt        int64               `json:"next_retry_at" gorm:"index"`
	InputExpiresAt     int64               `json:"input_expires_at" gorm:"index"`
	StartedAt          int64               `json:"started_at"`
	FinishedAt         int64               `json:"finished_at"`
	FailReason         string              `json:"fail_reason" gorm:"type:text"`
	RequestBody        json.RawMessage     `json:"-" gorm:"type:json"`
	PriceData          json.RawMessage     `json:"-" gorm:"type:json"`
	ResponseBody       json.RawMessage     `json:"response_body,omitempty" gorm:"type:json"`
	ResponseStatusCode int                 `json:"response_status_code"`
	Input              *GenerationJobInput `json:"input,omitempty" gorm:"-"`
}

type GenerationJobInput struct {
	Prompt             string   `json:"prompt,omitempty"`
	Size               string   `json:"size,omitempty"`
	ReferenceImageURLs []string `json:"reference_image_urls,omitempty"`
}

func NewGenerationJobID() string {
	key, _ := common.GenerateRandomCharsKey(32)
	return "gen_" + key
}

func CreateGenerationJob(job *GenerationJob) error {
	now := time.Now().Unix()
	job.CreatedAt = now
	job.UpdatedAt = now
	if job.JobID == "" {
		job.JobID = NewGenerationJobID()
	}
	if job.Status == "" {
		job.Status = GenerationJobStatusQueued
	}
	if job.NextRetryAt == 0 {
		job.NextRetryAt = now
	}
	return DB.Create(job).Error
}

func RetryGenerationJob(source *GenerationJob, jobID string, preConsumedQuota int, billingSource string, subscriptionId int) (*GenerationJob, error) {
	job := &GenerationJob{
		JobID:            jobID,
		UserId:           source.UserId,
		Group:            source.Group,
		ChannelId:        source.ChannelId,
		TokenId:          source.TokenId,
		Model:            source.Model,
		Path:             source.Path,
		ImageCount:       source.ImageCount,
		Quota:            preConsumedQuota,
		PreConsumedQuota: preConsumedQuota,
		BillingSource:    billingSource,
		SubscriptionId:   subscriptionId,
		InputExpiresAt:   source.InputExpiresAt,
		RequestBody:      source.RequestBody,
		PriceData:        source.PriceData,
	}
	if err := CreateGenerationJob(job); err != nil {
		return nil, err
	}
	return job, nil
}

func GetGenerationJobByPublicID(jobID string) (*GenerationJob, error) {
	job := &GenerationJob{}
	if err := DB.Where("job_id = ?", jobID).First(job).Error; err != nil {
		return nil, err
	}
	return job, nil
}

func GetUserGenerationJob(jobID string, userId int) (*GenerationJob, error) {
	job := &GenerationJob{}
	if err := DB.Where("job_id = ? AND user_id = ?", jobID, userId).First(job).Error; err != nil {
		return nil, err
	}
	return job, nil
}

func GetGenerationJobByID(id int64) (*GenerationJob, error) {
	job := &GenerationJob{}
	if err := DB.Where("id = ?", id).First(job).Error; err != nil {
		return nil, err
	}
	return job, nil
}

func GetUserGenerationJobs(userId int, startIdx int, num int, status string, path string) ([]*GenerationJob, int64, error) {
	var jobs []*GenerationJob
	query := DB.Model(&GenerationJob{}).Where("user_id = ?", userId)
	query = applyGenerationJobFilters(query, status, path)
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if err := query.Order("id desc").Limit(num).Offset(startIdx).Find(&jobs).Error; err != nil {
		return nil, 0, err
	}
	return jobs, total, nil
}

func GetAllGenerationJobs(startIdx int, num int, status string, path string, userId int) ([]*GenerationJob, int64, error) {
	var jobs []*GenerationJob
	query := DB.Model(&GenerationJob{})
	query = applyGenerationJobFilters(query, status, path)
	if userId > 0 {
		query = query.Where("user_id = ?", userId)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if err := query.Order("id desc").Limit(num).Offset(startIdx).Find(&jobs).Error; err != nil {
		return nil, 0, err
	}
	return jobs, total, nil
}

func applyGenerationJobFilters(query *gorm.DB, status string, path string) *gorm.DB {
	if status != "" {
		query = query.Where("status = ?", status)
	}
	if path != "" {
		query = query.Where("path = ?", path)
	}
	return query
}

func PickQueuedGenerationJob() (*GenerationJob, error) {
	now := time.Now().Unix()
	job := &GenerationJob{}
	err := DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("status = ? AND next_retry_at <= ?", GenerationJobStatusQueued, now).
			Order("next_retry_at asc, id asc").First(job).Error; err != nil {
			return err
		}
		updates := map[string]any{
			"status":     GenerationJobStatusRunning,
			"started_at": now,
			"updated_at": now,
		}
		result := tx.Model(&GenerationJob{}).
			Where("id = ? AND status = ?", job.ID, GenerationJobStatusQueued).
			Updates(updates)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return gorm.ErrRecordNotFound
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	job.Status = GenerationJobStatusRunning
	job.StartedAt = now
	job.UpdatedAt = now
	return job, nil
}

func RequeueGenerationJob(jobId int64, retryAfterSeconds int, reason string) error {
	if retryAfterSeconds <= 0 {
		retryAfterSeconds = 5
	}
	now := time.Now().Unix()
	return DB.Model(&GenerationJob{}).Where("id = ? AND status = ?", jobId, GenerationJobStatusRunning).Updates(map[string]any{
		"status":        GenerationJobStatusQueued,
		"retry_count":   gorm.Expr("retry_count + ?", 1),
		"next_retry_at": now + int64(retryAfterSeconds),
		"fail_reason":   reason,
		"updated_at":    now,
	}).Error
}

func ResetRunningGenerationJobs(reason string) error {
	now := time.Now().Unix()
	return DB.Model(&GenerationJob{}).Where("status = ?", GenerationJobStatusRunning).Updates(map[string]any{
		"status":        GenerationJobStatusQueued,
		"next_retry_at": now,
		"fail_reason":   reason,
		"updated_at":    now,
	}).Error
}

func CancelQueuedGenerationJob(jobID string, userId int) (*GenerationJob, bool, error) {
	now := time.Now().Unix()
	job := &GenerationJob{}
	err := DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("job_id = ? AND user_id = ?", jobID, userId).First(job).Error; err != nil {
			return err
		}
		if job.Status != GenerationJobStatusQueued {
			return nil
		}
		result := tx.Model(&GenerationJob{}).
			Where("id = ? AND status = ?", job.ID, GenerationJobStatusQueued).
			Updates(map[string]any{
				"status":      GenerationJobStatusCancelled,
				"fail_reason": "用户取消",
				"finished_at": now,
				"updated_at":  now,
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return nil
		}
		job.Status = GenerationJobStatusCancelled
		job.FinishedAt = now
		job.UpdatedAt = now
		return nil
	})
	if err != nil {
		return nil, false, err
	}
	return job, job.Status == GenerationJobStatusCancelled, nil
}

func CancelRunningGenerationJob(jobID string, userId int) (*GenerationJob, bool, error) {
	now := time.Now().Unix()
	job := &GenerationJob{}
	err := DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("job_id = ? AND user_id = ?", jobID, userId).First(job).Error; err != nil {
			return err
		}
		if job.Status != GenerationJobStatusRunning {
			return nil
		}
		result := tx.Model(&GenerationJob{}).
			Where("id = ? AND status = ?", job.ID, GenerationJobStatusRunning).
			Updates(map[string]any{
				"status":      GenerationJobStatusCancelled,
				"fail_reason": "开始运行后取消",
				"finished_at": now,
				"updated_at":  now,
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return nil
		}
		job.Status = GenerationJobStatusCancelled
		job.FinishedAt = now
		job.UpdatedAt = now
		return nil
	})
	if err != nil {
		return nil, false, err
	}
	return job, job.Status == GenerationJobStatusCancelled, nil
}

func CompleteGenerationJob(jobId int64, quota int, statusCode int, responseBody []byte) error {
	now := time.Now().Unix()
	return DB.Model(&GenerationJob{}).Where("id = ? AND status = ?", jobId, GenerationJobStatusRunning).Updates(map[string]any{
		"status":               GenerationJobStatusSucceeded,
		"quota":                quota,
		"response_status_code": statusCode,
		"response_body":        json.RawMessage(responseBody),
		"finished_at":          now,
		"updated_at":           now,
	}).Error
}

func FailGenerationJob(jobId int64, reason string, statusCode int) (bool, error) {
	now := time.Now().Unix()
	result := DB.Model(&GenerationJob{}).Where("id = ? AND status = ?", jobId, GenerationJobStatusRunning).Updates(map[string]any{
		"status":               GenerationJobStatusFailed,
		"response_status_code": statusCode,
		"fail_reason":          reason,
		"finished_at":          now,
		"updated_at":           now,
	})
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected > 0, nil
}
