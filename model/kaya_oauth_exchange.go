package model

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
)

const kayaOAuthExchangeCodeTTL = 5 * time.Minute

type KayaOAuthExchangeData struct {
	AppName string `json:"app_name"`
	UserId  int    `json:"user_id"`
}

type memoryKayaOAuthExchangeEntry struct {
	Data      KayaOAuthExchangeData
	ExpiresAt time.Time
}

var (
	memoryKayaOAuthExchangeCodes = map[string]memoryKayaOAuthExchangeEntry{}
	memoryKayaOAuthExchangeMutex sync.Mutex
)

func CreateKayaOAuthExchangeCode(userId int, appName string) (string, error) {
	if userId == 0 {
		return "", errors.New("user id is required")
	}
	code, err := common.GenerateRandomKey(32)
	if err != nil {
		return "", err
	}
	data := KayaOAuthExchangeData{AppName: appName, UserId: userId}
	encoded, err := common.Marshal(data)
	if err != nil {
		return "", err
	}

	if common.RedisEnabled {
		if err := common.RedisSet(kayaOAuthExchangeCodeKey(code), string(encoded), kayaOAuthExchangeCodeTTL); err != nil {
			return "", err
		}
		return code, nil
	}

	memoryKayaOAuthExchangeMutex.Lock()
	defer memoryKayaOAuthExchangeMutex.Unlock()
	memoryKayaOAuthExchangeCodes[code] = memoryKayaOAuthExchangeEntry{
		Data:      data,
		ExpiresAt: time.Now().Add(kayaOAuthExchangeCodeTTL),
	}
	return code, nil
}

func ConsumeKayaOAuthExchangeCode(code string) (*KayaOAuthExchangeData, error) {
	if code == "" {
		return nil, errors.New("code is required")
	}

	if common.RedisEnabled {
		key := kayaOAuthExchangeCodeKey(code)
		raw, err := common.RedisGet(key)
		if err != nil {
			return nil, err
		}
		_ = common.RedisDel(key)

		var data KayaOAuthExchangeData
		if err := common.Unmarshal([]byte(raw), &data); err != nil {
			return nil, err
		}
		return &data, nil
	}

	memoryKayaOAuthExchangeMutex.Lock()
	defer memoryKayaOAuthExchangeMutex.Unlock()
	entry, ok := memoryKayaOAuthExchangeCodes[code]
	if !ok {
		return nil, errors.New("code not found")
	}
	delete(memoryKayaOAuthExchangeCodes, code)
	if time.Now().After(entry.ExpiresAt) {
		return nil, errors.New("code expired")
	}
	return &entry.Data, nil
}

func kayaOAuthExchangeCodeKey(code string) string {
	return fmt.Sprintf("kaya:oauth_exchange:%s", code)
}
