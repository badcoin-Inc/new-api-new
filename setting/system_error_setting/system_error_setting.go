package system_error_setting

import (
	"strconv"
	"strings"
	"sync"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/config"
)

type SystemErrorMessageConfig struct {
	DefaultMessage    string            `json:"default_message"`
	ErrorCodeMapping  map[string]string `json:"error_code_mapping"`
	StatusCodeMapping map[string]string `json:"status_code_mapping"`
	mutex             sync.RWMutex      `json:"-"`
}

var globalSystemErrorConfig = &SystemErrorMessageConfig{
	DefaultMessage:    "",
	ErrorCodeMapping:  make(map[string]string),
	StatusCodeMapping: make(map[string]string),
}

func init() {
	config.GlobalConfig.Register("system_error_setting", globalSystemErrorConfig)
}

func GetDefaultMessage() string {
	globalSystemErrorConfig.mutex.RLock()
	defer globalSystemErrorConfig.mutex.RUnlock()
	return globalSystemErrorConfig.DefaultMessage
}

func GetMessageByErrorCode(errorCode string) string {
	globalSystemErrorConfig.mutex.RLock()
	defer globalSystemErrorConfig.mutex.RUnlock()
	return globalSystemErrorConfig.ErrorCodeMapping[errorCode]
}

func GetMessageByStatusCode(statusCode int) string {
	globalSystemErrorConfig.mutex.RLock()
	defer globalSystemErrorConfig.mutex.RUnlock()
	return globalSystemErrorConfig.StatusCodeMapping[strconv.Itoa(statusCode)]
}

func (c *SystemErrorMessageConfig) UpdateFromMap(configMap map[string]string) error {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	if value, ok := configMap["default_message"]; ok {
		c.DefaultMessage = value
	}
	if value, ok := configMap["error_code_mapping"]; ok {
		if strings.TrimSpace(value) == "" {
			c.ErrorCodeMapping = nil
		} else {
			mapping := make(map[string]string)
			if err := common.Unmarshal([]byte(value), &mapping); err == nil {
				c.ErrorCodeMapping = mapping
			}
		}
	}
	if value, ok := configMap["status_code_mapping"]; ok {
		if strings.TrimSpace(value) == "" {
			c.StatusCodeMapping = nil
			return nil
		}
		mapping := make(map[string]string)
		if err := common.Unmarshal([]byte(value), &mapping); err != nil {
			return nil
		}
		c.StatusCodeMapping = mapping
	}
	return nil
}

func (c *SystemErrorMessageConfig) ExportMap() (map[string]string, error) {
	c.mutex.RLock()
	defer c.mutex.RUnlock()

	errorCodeMapping, err := common.Marshal(c.ErrorCodeMapping)
	if err != nil {
		return nil, err
	}
	statusCodeMapping, err := common.Marshal(c.StatusCodeMapping)
	if err != nil {
		return nil, err
	}
	return map[string]string{
		"default_message":     c.DefaultMessage,
		"error_code_mapping":  string(errorCodeMapping),
		"status_code_mapping": string(statusCodeMapping),
	}, nil
}
