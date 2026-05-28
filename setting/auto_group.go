package setting

import (
	"strings"

	"github.com/QuantumNous/new-api/common"
)

var autoGroups = []string{
	"default",
}

var DefaultUseAutoGroup = false
var DefaultGeneratedTokenGroups = []string{}

func GetDefaultGeneratedTokenGroups() []string {
	if len(DefaultGeneratedTokenGroups) > 0 {
		return DefaultGeneratedTokenGroups
	}
	if DefaultUseAutoGroup {
		return []string{"auto"}
	}
	return []string{}
}

func UpdateDefaultGeneratedTokenGroupsByString(value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		DefaultGeneratedTokenGroups = []string{}
		return nil
	}
	// Try JSON array first
	if strings.HasPrefix(value, "[") {
		var groups []string
		if err := common.Unmarshal([]byte(value), &groups); err == nil {
			DefaultGeneratedTokenGroups = groups
			return nil
		}
	}
	// Fallback to comma-separated
	parts := strings.Split(value, ",")
	groups := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			groups = append(groups, p)
		}
	}
	DefaultGeneratedTokenGroups = groups
	return nil
}

func DefaultGeneratedTokenGroups2String() string {
	if len(DefaultGeneratedTokenGroups) == 0 {
		return ""
	}
	return strings.Join(DefaultGeneratedTokenGroups, ",")
}

func ContainsAutoGroup(group string) bool {
	for _, autoGroup := range autoGroups {
		if autoGroup == group {
			return true
		}
	}
	return false
}

func UpdateAutoGroupsByJsonString(jsonString string) error {
	autoGroups = make([]string, 0)
	return common.Unmarshal([]byte(jsonString), &autoGroups)
}

func AutoGroups2JsonString() string {
	jsonBytes, err := common.Marshal(autoGroups)
	if err != nil {
		return "[]"
	}
	return string(jsonBytes)
}

func GetAutoGroups() []string {
	return autoGroups
}
