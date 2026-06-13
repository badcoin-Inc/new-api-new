package setting

import (
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
)

var autoGroups = []string{
	"default",
}

var DefaultUseAutoGroup = false
var DefaultGeneratedTokenGroups = []string{}
var DefaultGeneratedTokenGroupsByApp = map[string][]string{}

func GetDefaultGeneratedTokenGroups() []string {
	if len(DefaultGeneratedTokenGroups) > 0 {
		return DefaultGeneratedTokenGroups
	}
	if DefaultUseAutoGroup {
		return []string{"auto"}
	}
	return []string{}
}

func GetDefaultGeneratedTokenGroupsForApp(appName string) []string {
	appName = strings.TrimSpace(appName)
	if appName != "" {
		if groups := getDefaultGeneratedTokenGroupsForAppFromEnv(appName); len(groups) > 0 {
			return groups
		}
		if groups, ok := DefaultGeneratedTokenGroupsByApp[appName]; ok && len(groups) > 0 {
			return groups
		}
	}
	return GetDefaultGeneratedTokenGroups()
}

func getDefaultGeneratedTokenGroupsForAppFromEnv(appName string) []string {
	value := strings.TrimSpace(constant.DefaultGeneratedTokenGroupsByAppEnv)
	if value == "" {
		return nil
	}
	groupsByApp := map[string][]string{}
	if err := common.Unmarshal([]byte(value), &groupsByApp); err != nil {
		common.SysError("failed to parse DEFAULT_GENERATED_TOKEN_GROUPS_BY_APP: " + err.Error())
		return nil
	}
	groups, ok := groupsByApp[appName]
	if !ok {
		return nil
	}
	cleanGroups := make([]string, 0, len(groups))
	for _, group := range groups {
		group = strings.TrimSpace(group)
		if group != "" {
			cleanGroups = append(cleanGroups, group)
		}
	}
	return cleanGroups
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

func UpdateDefaultGeneratedTokenGroupsByAppByJSONString(value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		DefaultGeneratedTokenGroupsByApp = map[string][]string{}
		return nil
	}

	groupsByApp := map[string][]string{}
	if err := common.Unmarshal([]byte(value), &groupsByApp); err != nil {
		return err
	}

	cleaned := make(map[string][]string, len(groupsByApp))
	for appName, groups := range groupsByApp {
		appName = strings.TrimSpace(appName)
		if appName == "" {
			continue
		}
		cleanGroups := make([]string, 0, len(groups))
		for _, group := range groups {
			group = strings.TrimSpace(group)
			if group != "" {
				cleanGroups = append(cleanGroups, group)
			}
		}
		if len(cleanGroups) > 0 {
			cleaned[appName] = cleanGroups
		}
	}
	DefaultGeneratedTokenGroupsByApp = cleaned
	return nil
}

func DefaultGeneratedTokenGroupsByApp2JSONString() string {
	jsonBytes, err := common.Marshal(DefaultGeneratedTokenGroupsByApp)
	if err != nil {
		return "{}"
	}
	return string(jsonBytes)
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
