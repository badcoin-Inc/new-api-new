package controller

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func CreateGenerationJob(c *gin.Context) {
	createGenerationJob(c, "/v1/images/generations")
}

func CreateGenerationEditJob(c *gin.Context) {
	createGenerationJob(c, "/v1/images/edits")
}

func createGenerationJob(c *gin.Context, jobPath string) {
	if !strings.HasPrefix(c.GetHeader("Content-Type"), gin.MIMEJSON) && c.GetHeader("Content-Type") != "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": "generation jobs only support JSON requests; upload images first and pass image_url", "type": "invalid_request"}})
		return
	}

	request, err := helper.GetAndValidateRequest(c, types.RelayFormatOpenAIImage)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": err.Error(), "type": "invalid_request"}})
		return
	}
	imageReq, ok := request.(*dto.ImageRequest)
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": "invalid image request", "type": "invalid_request"}})
		return
	}

	requestBody, err := generationJobRequestBody(c, jobPath, imageReq)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": err.Error(), "type": "invalid_request"}})
		return
	}

	relayInfo, err := relaycommon.GenRelayInfo(c, types.RelayFormatOpenAIImage, request, nil)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"message": err.Error(), "type": "gen_relay_info_failed"}})
		return
	}
	relayInfo.InitChannelMeta(c)
	meta := imageReq.GetTokenCountMeta()
	tokens, err := service.EstimateRequestToken(c, meta, relayInfo)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"message": err.Error(), "type": "count_token_failed"}})
		return
	}
	relayInfo.SetEstimatePromptTokens(tokens)
	priceData, err := helper.ModelPriceHelper(c, relayInfo, tokens, meta)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": err.Error(), "type": "model_price_error"}})
		return
	}
	relayInfo.ForcePreConsume = true
	relayInfo.PriceData = priceData
	if !priceData.FreeModel {
		if apiErr := service.PreConsumeBilling(c, priceData.QuotaToPreConsume, relayInfo); apiErr != nil {
			c.JSON(apiErr.StatusCode, gin.H{"error": apiErr.ToOpenAIError()})
			return
		}
	}

	priceSnapshot, err := common.Marshal(priceData)
	if err != nil {
		if relayInfo.Billing != nil {
			relayInfo.Billing.Refund(c)
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"message": err.Error(), "type": "marshal_price_failed"}})
		return
	}
	preConsumed := relayInfo.FinalPreConsumedQuota
	job := &model.GenerationJob{
		UserId:           relayInfo.UserId,
		Group:            relayInfo.UsingGroup,
		ChannelId:        relayInfo.ChannelId,
		TokenId:          relayInfo.TokenId,
		Model:            relayInfo.OriginModelName,
		Path:             jobPath,
		ImageCount:       generationJobImageCount(imageReq),
		Quota:            preConsumed,
		PreConsumedQuota: preConsumed,
		BillingSource:    relayInfo.BillingSource,
		SubscriptionId:   relayInfo.SubscriptionId,
		InputExpiresAt:   generationJobInputExpiresAt(jobPath, c.GetHeader("Content-Type")),
		RequestBody:      requestBody,
		PriceData:        priceSnapshot,
	}
	if err := model.CreateGenerationJob(job); err != nil {
		if relayInfo.Billing != nil {
			relayInfo.Billing.Refund(c)
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"message": err.Error(), "type": "create_generation_job_failed"}})
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"id": job.JobID, "status": job.Status, "next_retry_at": job.NextRetryAt})
}

func generationJobImageCount(imageReq *dto.ImageRequest) int {
	if imageReq == nil || imageReq.N == nil || *imageReq.N == 0 {
		return 1
	}
	return int(*imageReq.N)
}

func generationJobInputExpiresAt(jobPath string, contentType string) int64 {
	return 0
}

func generationJobRequestBody(c *gin.Context, jobPath string, imageReq *dto.ImageRequest) ([]byte, error) {
	storage, err := common.GetBodyStorage(c)
	if err != nil {
		return nil, err
	}
	return storage.Bytes()
}

func uploadMultipartImage(c *gin.Context, fileHeader *multipart.FileHeader) (string, error) {
	if fileHeader == nil {
		return "", errors.New("image file is required")
	}
	file, err := fileHeader.Open()
	if err != nil {
		return "", err
	}
	defer file.Close()
	data, err := io.ReadAll(file)
	if err != nil {
		return "", err
	}
	contentType := fileHeader.Header.Get("Content-Type")
	return service.UploadGenerationJobObject(c.Request.Context(), data, fileHeader.Filename, contentType)
}

func GetGenerationJob(c *gin.Context) {
	job, err := model.GetUserGenerationJob(c.Param("id"), c.GetInt("id"))
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, gorm.ErrRecordNotFound) {
			status = http.StatusNotFound
		}
		c.JSON(status, gin.H{"error": gin.H{"message": "generation job not found", "type": "not_found"}})
		return
	}
	fillGenerationJobInput(job)
	c.JSON(http.StatusOK, job)
}

func GetUserGenerationJobs(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)
	jobs, total, err := model.GetUserGenerationJobs(c.GetInt("id"), pageInfo.GetStartIdx(), pageInfo.GetPageSize(), c.Query("status"), c.Query("path"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"message": err.Error(), "type": "list_generation_jobs_failed"}})
		return
	}
	fillGenerationJobsInput(jobs)
	c.JSON(http.StatusOK, gin.H{
		"items":     jobs,
		"total":     total,
		"page":      pageInfo.GetPage(),
		"page_size": pageInfo.GetPageSize(),
	})
}

func GetAllGenerationJobs(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)
	userId, _ := strconv.Atoi(c.DefaultQuery("user_id", "0"))
	jobs, total, err := model.GetAllGenerationJobs(pageInfo.GetStartIdx(), pageInfo.GetPageSize(), c.Query("status"), c.Query("path"), userId)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"message": err.Error(), "type": "list_generation_jobs_failed"}})
		return
	}
	filterGenerationJobsPrivateDataForUser(jobs, c.GetInt("id"))
	c.JSON(http.StatusOK, gin.H{
		"items":     jobs,
		"total":     total,
		"page":      pageInfo.GetPage(),
		"page_size": pageInfo.GetPageSize(),
	})
}

func fillGenerationJobsInput(jobs []*model.GenerationJob) {
	for _, job := range jobs {
		fillGenerationJobInput(job)
	}
}

func filterGenerationJobsPrivateDataForUser(jobs []*model.GenerationJob, userId int) {
	for _, job := range jobs {
		if job.UserId == userId {
			fillGenerationJobInput(job)
			continue
		}
		job.Input = nil
		job.ResponseBody = nil
	}
}

func fillGenerationJobInput(job *model.GenerationJob) {
	job.Input = generationJobInputFromBody(job.RequestBody)
}

func generationJobInputFromBody(body json.RawMessage) *model.GenerationJobInput {
	if len(body) == 0 {
		return nil
	}
	var request struct {
		Prompt string          `json:"prompt"`
		Size   string          `json:"size"`
		Images json.RawMessage `json:"images"`
		Image  json.RawMessage `json:"image"`
	}
	if err := common.Unmarshal(body, &request); err != nil {
		return nil
	}
	input := &model.GenerationJobInput{
		Prompt:             strings.TrimSpace(request.Prompt),
		Size:               strings.TrimSpace(request.Size),
		ReferenceImageURLs: appendImageURLs(nil, request.Images),
	}
	input.ReferenceImageURLs = appendImageURLs(input.ReferenceImageURLs, request.Image)
	if input.Prompt == "" && input.Size == "" && len(input.ReferenceImageURLs) == 0 {
		return nil
	}
	return input
}

func appendImageURLs(urls []string, raw json.RawMessage) []string {
	if len(raw) == 0 || string(raw) == "null" {
		return urls
	}
	var items []any
	if err := common.Unmarshal(raw, &items); err == nil {
		for _, item := range items {
			urls = appendImageURL(urls, item)
		}
		return urls
	}
	var item any
	if err := common.Unmarshal(raw, &item); err != nil {
		return urls
	}
	return appendImageURL(urls, item)
}

func appendImageURL(urls []string, item any) []string {
	switch value := item.(type) {
	case string:
		if strings.TrimSpace(value) != "" {
			urls = append(urls, strings.TrimSpace(value))
		}
	case map[string]any:
		if imageURL, ok := value["image_url"].(string); ok && strings.TrimSpace(imageURL) != "" {
			urls = append(urls, strings.TrimSpace(imageURL))
		} else if url, ok := value["url"].(string); ok && strings.TrimSpace(url) != "" {
			urls = append(urls, strings.TrimSpace(url))
		}
	}
	return urls
}

func UploadGenerationJobImage(c *gin.Context) {
	if !service.LoadR2UploadConfig().Enabled() {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": "R2 public upload config is required", "type": "r2_required"}})
		return
	}
	fileHeader, err := c.FormFile("image")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": "image file is required", "type": "invalid_request"}})
		return
	}
	url, err := uploadMultipartImage(c, fileHeader)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": err.Error(), "type": "upload_image_failed"}})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"url":        url,
		"expires_at": service.R2UploadInputExpiresAt(),
	})
}

func CancelGenerationJob(c *gin.Context) {
	job, refunded, err := model.CancelQueuedGenerationJob(c.Param("id"), c.GetInt("id"))
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, gorm.ErrRecordNotFound) {
			status = http.StatusNotFound
		}
		c.JSON(status, gin.H{"error": gin.H{"message": "generation job not found", "type": "not_found"}})
		return
	}
	if job == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": gin.H{"message": "generation job not found", "type": "not_found"}})
		return
	}
	if refunded {
		refundGenerationJob(job, "generation job cancelled before running")
		c.JSON(http.StatusOK, gin.H{"id": job.JobID, "status": job.Status, "refunded": true})
		return
	}
	job, cancelled, err := model.CancelRunningGenerationJob(c.Param("id"), c.GetInt("id"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"message": err.Error(), "type": "cancel_generation_job_failed"}})
		return
	}
	if cancelled {
		c.JSON(http.StatusOK, gin.H{"id": job.JobID, "status": job.Status, "refunded": false})
		return
	}
	c.JSON(http.StatusConflict, gin.H{"error": gin.H{"message": "generation job cannot be cancelled in current status", "type": "invalid_job_status"}, "status": job.Status})
}

func RetryGenerationJob(c *gin.Context) {
	job, err := model.GetUserGenerationJob(c.Param("id"), c.GetInt("id"))
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, gorm.ErrRecordNotFound) {
			status = http.StatusNotFound
		}
		c.JSON(status, gin.H{"error": gin.H{"message": "generation job not found", "type": "not_found"}})
		return
	}
	if job.Status != model.GenerationJobStatusSucceeded && job.Status != model.GenerationJobStatusFailed && job.Status != model.GenerationJobStatusCancelled {
		c.JSON(http.StatusConflict, gin.H{"error": gin.H{"message": "generation job cannot be retried in current status", "type": "invalid_job_status"}, "status": job.Status})
		return
	}
	if job.InputExpiresAt > 0 && job.InputExpiresAt <= time.Now().Unix() {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": "generation job input image expired", "type": "input_expired"}})
		return
	}
	token, err := model.GetTokenById(job.TokenId)
	if err != nil || token.UserId != job.UserId {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": "generation job token is unavailable", "type": "invalid_token"}})
		return
	}
	userSetting, _ := model.GetUserSetting(job.UserId, false)
	jobID := model.NewGenerationJobID()
	relayInfo := &relaycommon.RelayInfo{
		RequestId:             jobID,
		UserId:                job.UserId,
		UsingGroup:            job.Group,
		UserGroup:             job.Group,
		TokenId:               job.TokenId,
		TokenKey:              token.Key,
		TokenUnlimited:        token.UnlimitedQuota,
		OriginModelName:       job.Model,
		ForcePreConsume:       true,
		UserSetting:           userSetting,
		BillingSource:         job.BillingSource,
		SubscriptionId:        job.SubscriptionId,
		FinalPreConsumedQuota: job.PreConsumedQuota,
	}
	if job.PreConsumedQuota > 0 {
		if apiErr := service.PreConsumeBilling(c, job.PreConsumedQuota, relayInfo); apiErr != nil {
			statusCode := apiErr.StatusCode
			if statusCode == 0 {
				statusCode = http.StatusInternalServerError
			}
			c.JSON(statusCode, gin.H{"error": apiErr.ToOpenAIError()})
			return
		}
	}
	newJob, err := model.RetryGenerationJob(job, jobID, relayInfo.FinalPreConsumedQuota, relayInfo.BillingSource, relayInfo.SubscriptionId)
	if err != nil {
		if relayInfo.Billing != nil {
			relayInfo.Billing.Refund(c)
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"message": err.Error(), "type": "retry_generation_job_failed"}})
		return
	}
	fillGenerationJobInput(newJob)
	c.JSON(http.StatusAccepted, newJob)
}

func StartGenerationJobWorker() {
	concurrency := common.GetEnvOrDefault("GENERATION_JOB_WORKER_CONCURRENCY", 1)
	if concurrency <= 0 {
		return
	}
	if err := model.ResetRunningGenerationJobs("reset running generation jobs on startup"); err != nil {
		common.SysLog("reset running generation jobs failed: " + err.Error())
	}
	for i := 0; i < concurrency; i++ {
		go generationJobWorkerLoop()
	}
}

func generationJobWorkerLoop() {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for range ticker.C {
		job, err := model.PickQueuedGenerationJob()
		if err != nil {
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				common.SysLog("pick generation job failed: " + err.Error())
			}
			continue
		}
		processGenerationJob(job)
	}
}

func processGenerationJob(job *model.GenerationJob) {
	if generationJobTimedOut(job) {
		message := "generation job timed out"
		failGenerationJobWithRefund(job, http.StatusRequestTimeout, message)
		return
	}
	if job.InputExpiresAt > 0 && job.InputExpiresAt <= time.Now().Unix() {
		message := "generation job input image expired"
		failGenerationJobWithRefund(job, http.StatusBadRequest, message)
		return
	}
	release, acquired := service.AcquireGenerationJobDownstreamSlot()
	if !acquired {
		_ = model.RequeueGenerationJob(job.ID, common.GetEnvOrDefault("SUB2API_JOB_RETRY_AFTER_SECONDS", 5), "local generation job concurrency limit reached")
		return
	}
	if release != nil {
		defer release()
	}

	statusCode, responseBody, retryAfter, actualQuota, failReason, err := runGenerationJobRequest(job)
	if err == nil && statusCode >= 200 && statusCode < 300 {
		_ = model.CompleteGenerationJob(job.ID, actualQuota, statusCode, responseBody)
		return
	}
	message := "generation job failed"
	if failReason != "" {
		message = failReason
	} else if len(responseBody) > 0 {
		message = string(responseBody)
	} else if err != nil {
		message = err.Error()
	}
	if statusCode == http.StatusTooManyRequests {
		_ = model.RequeueGenerationJob(job.ID, retryAfter, message)
		return
	}
	failGenerationJobWithRefund(job, statusCode, message)
}

func failGenerationJobWithRefund(job *model.GenerationJob, statusCode int, message string) {
	failed, err := model.FailGenerationJob(job.ID, message, statusCode)
	if err != nil {
		logger.LogWarn(context.Background(), fmt.Sprintf("fail generation job status update failed job=%s: %s", job.JobID, err.Error()))
		return
	}
	if !failed {
		return
	}
	refundGenerationJob(job, message)
}

func generationJobTimedOut(job *model.GenerationJob) bool {
	timeoutSeconds := common.GetEnvOrDefault("GENERATION_JOB_TIMEOUT_SECONDS", 0)
	return timeoutSeconds > 0 && job.CreatedAt > 0 && time.Now().Unix()-job.CreatedAt >= int64(timeoutSeconds)
}

func generationJobDeadline(job *model.GenerationJob) time.Time {
	timeoutSeconds := common.GetEnvOrDefault("GENERATION_JOB_TIMEOUT_SECONDS", 0)
	if timeoutSeconds <= 0 || job.CreatedAt <= 0 {
		return time.Time{}
	}
	return time.Unix(job.CreatedAt+int64(timeoutSeconds), 0)
}

func runGenerationJobRequest(job *model.GenerationJob) (int, []byte, int, int, string, error) {
	var imageReq dto.ImageRequest
	if err := common.Unmarshal(job.RequestBody, &imageReq); err != nil {
		return 0, nil, 0, 0, "", err
	}
	var priceData types.PriceData
	if len(job.PriceData) > 0 {
		if err := common.Unmarshal(job.PriceData, &priceData); err != nil {
			return 0, nil, 0, 0, "", err
		}
	}
	channel, err := model.GetChannelById(job.ChannelId, true)
	if err != nil {
		return 0, nil, 0, 0, "", err
	}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	reqCtx := context.Background()
	if deadline := generationJobDeadline(job); !deadline.IsZero() {
		var cancel context.CancelFunc
		reqCtx, cancel = context.WithDeadline(reqCtx, deadline)
		defer cancel()
	}
	c.Request = httptest.NewRequest(http.MethodPost, job.Path, bytes.NewReader(job.RequestBody)).WithContext(reqCtx)
	c.Request.Header.Set("Content-Type", gin.MIMEJSON)
	c.Set(common.RequestIdKey, job.JobID)
	common.SetContextKey(c, constant.ContextKeyUserId, job.UserId)
	if username, err := model.GetUsernameById(job.UserId, false); err == nil {
		c.Set("username", username)
		common.SetContextKey(c, constant.ContextKeyUserName, username)
	}
	common.SetContextKey(c, constant.ContextKeyUsingGroup, job.Group)
	common.SetContextKey(c, constant.ContextKeyUserGroup, job.Group)
	common.SetContextKey(c, constant.ContextKeyTokenId, job.TokenId)
	tokenKey := ""
	if token, err := model.GetTokenById(job.TokenId); err == nil {
		tokenKey = token.Key
		common.SetContextKey(c, constant.ContextKeyTokenKey, tokenKey)
		c.Set("token_name", token.Name)
	}
	if apiErr := middleware.SetupContextForSelectedChannel(c, channel, job.Model); apiErr != nil {
		return 0, nil, 0, 0, apiErr.MaskSensitiveOriginalErrorWithStatusCode(), apiErr
	}
	billing := &generationJobBillingSession{job: job, tokenKey: tokenKey, actualQuota: job.PreConsumedQuota}
	relayInfo := relaycommon.GenRelayInfoImage(c, &imageReq)
	relayInfo.InitChannelMeta(c)
	relayInfo.ForcePreConsume = true
	relayInfo.PriceData = priceData
	relayInfo.TieredBillingSnapshot = priceData.TieredBillingSnapshot
	relayInfo.BillingRequestInput = priceData.BillingRequestInput
	relayInfo.FinalPreConsumedQuota = job.PreConsumedQuota
	relayInfo.Billing = billing
	relayInfo.BillingSource = job.BillingSource
	relayInfo.SubscriptionId = job.SubscriptionId
	relayInfo.ChannelId = job.ChannelId
	relayInfo.TokenId = job.TokenId
	relayInfo.OriginModelName = job.Model

	newAPIError := relay.ImageHelper(c, relayInfo)
	if newAPIError != nil {
		return newAPIError.StatusCode, w.Body.Bytes(), parseRecorderRetryAfter(w), billing.actualQuota, newAPIError.MaskSensitiveOriginalErrorWithStatusCode(), newAPIError
	}
	return w.Code, w.Body.Bytes(), parseRecorderRetryAfter(w), billing.actualQuota, "", nil
}

type generationJobBillingSession struct {
	job         *model.GenerationJob
	tokenKey    string
	actualQuota int
	settled     bool
	refunded    bool
	mu          sync.Mutex
}

func (s *generationJobBillingSession) Settle(actualQuota int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.settled || s.refunded {
		return nil
	}
	delta := actualQuota - s.job.PreConsumedQuota
	if delta != 0 {
		if s.job.BillingSource == service.BillingSourceSubscription && s.job.SubscriptionId > 0 {
			if err := model.PostConsumeUserSubscriptionDelta(s.job.SubscriptionId, int64(delta)); err != nil {
				return err
			}
		} else if delta > 0 {
			if err := model.DecreaseUserQuota(s.job.UserId, delta, false); err != nil {
				return err
			}
		} else if err := model.IncreaseUserQuota(s.job.UserId, -delta, false); err != nil {
			return err
		}
		if s.job.TokenId > 0 && s.tokenKey != "" {
			if delta > 0 {
				_ = model.DecreaseTokenQuota(s.job.TokenId, s.tokenKey, delta)
			} else {
				_ = model.IncreaseTokenQuota(s.job.TokenId, s.tokenKey, -delta)
			}
		}
	}
	s.actualQuota = actualQuota
	s.settled = true
	return nil
}

func (s *generationJobBillingSession) Refund(c *gin.Context) {
	s.mu.Lock()
	if s.settled || s.refunded {
		s.mu.Unlock()
		return
	}
	s.refunded = true
	s.mu.Unlock()
	refundGenerationJob(s.job, "generation job billing session refund")
}

func (s *generationJobBillingSession) NeedsRefund() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return !s.settled && !s.refunded && s.job.PreConsumedQuota > 0
}

func (s *generationJobBillingSession) GetPreConsumedQuota() int {
	return s.job.PreConsumedQuota
}

func (s *generationJobBillingSession) Reserve(targetQuota int) error {
	return nil
}

func parseRecorderRetryAfter(w *httptest.ResponseRecorder) int {
	seconds, _ := strconv.Atoi(strings.TrimSpace(w.Header().Get("Retry-After")))
	return service.FirstRetryAfterSeconds(seconds, w.Body.Bytes(), common.GetEnvOrDefault("SUB2API_JOB_RETRY_AFTER_SECONDS", 5))
}

var refundGenerationJobLocks sync.Map

func refundGenerationJob(job *model.GenerationJob, reason string) {
	if job.PreConsumedQuota <= 0 {
		return
	}
	lockKey := fmt.Sprintf("generation_job_refund_%d", job.ID)
	if _, loaded := refundGenerationJobLocks.LoadOrStore(lockKey, true); loaded {
		return
	}
	defer refundGenerationJobLocks.Delete(lockKey)
	if job.BillingSource == service.BillingSourceSubscription && job.SubscriptionId > 0 {
		if err := model.PostConsumeUserSubscriptionDelta(job.SubscriptionId, -int64(job.PreConsumedQuota)); err != nil {
			logger.LogWarn(context.Background(), fmt.Sprintf("refund generation job subscription failed job=%s: %s", job.JobID, err.Error()))
			return
		}
	} else if err := model.IncreaseUserQuota(job.UserId, job.PreConsumedQuota, false); err != nil {
		logger.LogWarn(context.Background(), fmt.Sprintf("refund generation job wallet failed job=%s: %s", job.JobID, err.Error()))
		return
	}
	if job.TokenId > 0 {
		if token, err := model.GetTokenById(job.TokenId); err == nil {
			_ = model.IncreaseTokenQuota(job.TokenId, token.Key, job.PreConsumedQuota)
		}
	}
	logger.LogInfo(context.Background(), fmt.Sprintf("generation job %s refunded: %s", job.JobID, reason))
}
