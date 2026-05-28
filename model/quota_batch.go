package model

import (
	"fmt"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"gorm.io/gorm"
)

type QuotaBatch struct {
	Id        int    `json:"id" gorm:"primaryKey;autoIncrement"`
	UserId    int    `json:"user_id" gorm:"index;not null"`
	Amount    int    `json:"amount" gorm:"not null"`
	Consumed  int    `json:"consumed" gorm:"default:0"`
	ExpireAt  int64  `json:"expire_at" gorm:"bigint;index;not null"`
	ExpiredAt int64  `json:"expired_at" gorm:"bigint;default:0"`
	Source    string `json:"source" gorm:"type:varchar(32);default:'topup'"`
	SourceId  int    `json:"source_id" gorm:"default:0"`
	CreatedAt int64  `json:"created_at" gorm:"bigint;index;not null"`
}

// CreateQuotaBatch records a new quota grant. Skipped when QUOTA_EXPIRE_DAYS == 0.
func CreateQuotaBatch(userId, amount, sourceId int, source string) {
	if common.QuotaExpireDays <= 0 || amount <= 0 {
		return
	}
	expireAt := time.Now().AddDate(0, 0, common.QuotaExpireDays).Unix()
	batch := &QuotaBatch{
		UserId:    userId,
		Amount:    amount,
		ExpireAt:  expireAt,
		Source:    source,
		SourceId:  sourceId,
		CreatedAt: common.GetTimestamp(),
	}
	if err := DB.Create(batch).Error; err != nil {
		common.SysLog("failed to create quota batch: " + err.Error())
	}
}

// SettleExpiredBatches finds all due batches, computes FIFO consumption attribution
// using the Log table, deducts the unspent remainder from User.quota, and marks
// each batch as settled. Returns the number of batches settled.
func SettleExpiredBatches() (settled int) {
	now := common.GetTimestamp()

	var batches []QuotaBatch
	if err := DB.Where("expire_at <= ? AND expired_at = 0", now).
		Order("expire_at asc").
		Find(&batches).Error; err != nil {
		common.SysLog("quota_batch: failed to query expired batches: " + err.Error())
		return
	}

	for i := range batches {
		batch := &batches[i]

		// Earliest batch created_at for this user — FIFO window starts here.
		var firstCreatedAt int64
		DB.Model(&QuotaBatch{}).
			Select("min(created_at)").
			Where("user_id = ?", batch.UserId).
			Scan(&firstCreatedAt)

		// Total quota consumed from window start to this batch's expire_at.
		var totalConsumed int64
		LOG_DB.Model(&Log{}).
			Select("coalesce(sum(quota), 0)").
			Where("user_id = ? AND type = ? AND created_at >= ? AND created_at <= ?",
				batch.UserId, LogTypeConsume, firstCreatedAt, batch.ExpireAt).
			Scan(&totalConsumed)

		// Consumption already attributed to earlier-expiring settled batches.
		var priorConsumed int64
		DB.Model(&QuotaBatch{}).
			Select("coalesce(sum(consumed), 0)").
			Where(
				"user_id = ? AND expired_at > 0 AND (expire_at < ? OR (expire_at = ? AND id < ?))",
				batch.UserId,
				batch.ExpireAt,
				batch.ExpireAt,
				batch.Id,
			).
			Scan(&priorConsumed)

		attributable := int(totalConsumed) - int(priorConsumed)
		if attributable < 0 {
			attributable = 0
		}
		batch.Consumed = min(batch.Amount, attributable)
		remaining := batch.Amount - batch.Consumed

		err := DB.Transaction(func(tx *gorm.DB) error {
			if remaining > 0 {
				// Read current quota inside the transaction to avoid going negative.
				var user User
				if err := tx.Select("quota").Where("id = ?", batch.UserId).First(&user).Error; err != nil {
					return err
				}
				newQuota := user.Quota - remaining
				if newQuota < 0 {
					newQuota = 0
				}
				if err := tx.Model(&User{}).Where("id = ?", batch.UserId).
					Update("quota", newQuota).Error; err != nil {
					return err
				}
			}
			batch.ExpiredAt = now
			return tx.Save(batch).Error
		})

		if err != nil {
			common.SysLog(fmt.Sprintf("quota_batch: failed to settle batch %d: %s", batch.Id, err.Error()))
			continue
		}

		if remaining > 0 {
			username, _ := GetUsernameById(batch.UserId, false)
			expireLog := &Log{
				UserId:    batch.UserId,
				Username:  username,
				CreatedAt: common.GetTimestamp(),
				Type:      LogTypeManage,
				Content:   fmt.Sprintf("额度批次到期（批次 #%d），扣减 %s", batch.Id, logger.LogQuota(remaining)),
				Quota:     remaining,
			}
			if err := LOG_DB.Create(expireLog).Error; err != nil {
				common.SysLog(fmt.Sprintf("quota_batch: failed to record expiry log for batch %d: %s", batch.Id, err.Error()))
			}
		}
		settled++
	}
	return
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func GetActiveQuotaBatches(userId int) ([]QuotaBatch, error) {
	now := common.GetTimestamp()
	var batches []QuotaBatch
	err := DB.Where("user_id = ? AND expired_at = 0 AND expire_at > ?", userId, now).
		Order("expire_at asc").
		Find(&batches).Error
	return batches, err
}

// QuotaBatchSummary is returned to callers that only need expiry metadata.
type QuotaBatchSummary struct {
	EarliestExpireAt int64 `json:"earliest_expire_at"` // 0 if no active batches
}

func GetQuotaBatchSummary(userId int) QuotaBatchSummary {
	if common.QuotaExpireDays <= 0 {
		return QuotaBatchSummary{}
	}
	batches, err := GetActiveQuotaBatches(userId)
	if err != nil || len(batches) == 0 {
		return QuotaBatchSummary{}
	}
	return QuotaBatchSummary{EarliestExpireAt: batches[0].ExpireAt}
}

// BatchSource constants.
const (
	BatchSourceTopup  = "topup"
	BatchSourceRedeem = "redeem"
)

// wrapQuotaBatchExpiry returns a human-readable expiry duration for display.
func QuotaExpireDaysText() string {
	d := common.QuotaExpireDays
	if d <= 0 {
		return ""
	}
	if d%365 == 0 {
		years := d / 365
		if years == 1 {
			return "1 年"
		}
		return fmt.Sprintf("%d 年", years)
	}
	if d%30 == 0 {
		return fmt.Sprintf("%d 个月", d/30)
	}
	return fmt.Sprintf("%d 天", d)
}
