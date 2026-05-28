package service

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
)

const quotaExpireTickInterval = 6 * time.Hour

var quotaExpireOnce sync.Once

func StartQuotaExpireTask() {
	if common.QuotaExpireDays <= 0 {
		return
	}
	quotaExpireOnce.Do(func() {
		if !common.IsMasterNode {
			return
		}
		go func() {
			logger.LogInfo(context.Background(), fmt.Sprintf(
				"quota expire task started: expire_days=%d tick=%s",
				common.QuotaExpireDays, quotaExpireTickInterval,
			))
			runQuotaExpireOnce()
			ticker := time.NewTicker(quotaExpireTickInterval)
			defer ticker.Stop()
			for range ticker.C {
				runQuotaExpireOnce()
			}
		}()
	})
}

func runQuotaExpireOnce() {
	settled := model.SettleExpiredBatches()
	if settled > 0 {
		logger.LogInfo(context.Background(), fmt.Sprintf("quota expire task: settled %d batch(es)", settled))
	}
}
