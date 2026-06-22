package service

import (
	"sync"

	"github.com/QuantumNous/new-api/common"
)

type sub2APISlotLimiter struct {
	once  sync.Once
	limit int
	ch    chan struct{}
}

func (l *sub2APISlotLimiter) acquire(envName string) (func(), bool) {
	l.once.Do(func() {
		l.limit = common.GetEnvOrDefault(envName, 0)
		if l.limit > 0 {
			l.ch = make(chan struct{}, l.limit)
		}
	})
	if l.ch == nil {
		return nil, true
	}
	select {
	case l.ch <- struct{}{}:
		var releaseOnce sync.Once
		return func() {
			releaseOnce.Do(func() {
				<-l.ch
			})
		}, true
	default:
		return nil, false
	}
}

var (
	sub2APISyncLimiter             sub2APISlotLimiter
	sub2APIJobLimiter              sub2APISlotLimiter
	generationJobDownstreamLimiter sub2APISlotLimiter
)

func AcquireSub2APISyncSlot() (func(), bool) {
	return sub2APISyncLimiter.acquire("SUB2API_SYNC_CONCURRENCY")
}

func AcquireSub2APIJobSlot() (func(), bool) {
	return sub2APIJobLimiter.acquire("SUB2API_JOB_CONCURRENCY")
}

func AcquireGenerationJobDownstreamSlot() (func(), bool) {
	return generationJobDownstreamLimiter.acquire("GENERATION_JOB_DOWNSTREAM_CONCURRENCY")
}
