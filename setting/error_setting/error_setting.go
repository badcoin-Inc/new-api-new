package error_setting

import (
	"strconv"
	"strings"
	"sync"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/config"
)

type ErrorMessageConfig struct {
	DefaultMessage    string            `json:"default_message"`
	StatusCodeMapping map[string]string `json:"status_code_mapping"`
	mutex             sync.RWMutex      `json:"-"`
}

var globalErrorConfig = &ErrorMessageConfig{
	DefaultMessage:    "",
	StatusCodeMapping: make(map[string]string),
}

func init() {
	config.GlobalConfig.Register("error_setting", globalErrorConfig)
}

func GetDefaultMessage() string {
	globalErrorConfig.mutex.RLock()
	defer globalErrorConfig.mutex.RUnlock()
	return globalErrorConfig.DefaultMessage
}

func GetMessageByStatusCode(statusCode int) string {
	globalErrorConfig.mutex.RLock()
	defer globalErrorConfig.mutex.RUnlock()
	return globalErrorConfig.StatusCodeMapping[strconv.Itoa(statusCode)]
}

func (c *ErrorMessageConfig) UpdateFromMap(configMap map[string]string) error {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	if value, ok := configMap["default_message"]; ok {
		c.DefaultMessage = value
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

func (c *ErrorMessageConfig) ExportMap() (map[string]string, error) {
	c.mutex.RLock()
	defer c.mutex.RUnlock()

	statusCodeMapping, err := common.Marshal(c.StatusCodeMapping)
	if err != nil {
		return nil, err
	}
	return map[string]string{
		"default_message":     c.DefaultMessage,
		"status_code_mapping": string(statusCodeMapping),
	}, nil
}
