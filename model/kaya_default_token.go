package model

import (
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/setting"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const KayaDefaultTokenNamePrefix = "Kaya Default Token"
const KayaDefaultTokenAppName = "Kaya"

func IsKayaDefaultTokenApp(appName string) bool {
	return strings.EqualFold(strings.TrimSpace(appName), KayaDefaultTokenAppName)
}

func appDefaultTokenNamePrefix(appName string) string {
	cleanAppName := strings.TrimSpace(appName)
	if cleanAppName == "" {
		return KayaDefaultTokenNamePrefix
	}
	return fmt.Sprintf("%s Default Token", sanitizeDefaultTokenAppName(cleanAppName))
}

func sanitizeDefaultTokenAppName(appName string) string {
	return strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == ' ' {
			return r
		}
		return '-'
	}, appName)
}

// EnsureKayaDefaultTokens creates idempotent default tokens for Kaya usage.
// The tokens never expire and have unlimited quota so Kaya can use them as the
// default credentials for chat, generation, agents and tools.
// Returns the list of newly created tokens (excludes already existing ones).
func EnsureKayaDefaultTokens(userId int, appName ...string) ([]*Token, error) {
	if userId == 0 || !constant.GenerateDefaultToken {
		return nil, nil
	}

	app := ""
	if len(appName) > 0 {
		app = appName[0]
	}
	groups := setting.GetDefaultGeneratedTokenGroupsForApp(app)
	if len(groups) == 0 {
		return nil, nil
	}
	prefix := KayaDefaultTokenNamePrefix
	if app != "" {
		prefix = appDefaultTokenNamePrefix(app)
	}

	var result []*Token
	err := DB.Transaction(func(tx *gorm.DB) error {
		// Serialize default-token creation per user without adding schema changes.
		var user User
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", userId).First(&user).Error; err != nil {
			return err
		}

		// Find existing tokens by group so one app does not create duplicate keys
		// when a user already has a usable token for the same group.
		var existingTokens []Token
		if err := tx.Where(fmt.Sprintf("user_id = ? AND %s IN ?", commonGroupCol), userId, groups).Find(&existingTokens).Error; err != nil {
			return err
		}

		existingMap := make(map[string]bool)
		for _, t := range existingTokens {
			existingMap[t.Group] = true
		}

		// Build tokens to insert
		now := common.GetTimestamp()
		var newTokens []*Token
		for _, group := range groups {
			name := fmt.Sprintf("%s (%s)", prefix, group)
			if existingMap[group] {
				continue
			}

			key, err := common.GenerateKey()
			if err != nil {
				return fmt.Errorf("failed to generate kaya default token key: %w", err)
			}

			newTokens = append(newTokens, &Token{
				UserId:             userId,
				Name:               name,
				Key:                key,
				Group:              group,
				CreatedTime:        now,
				AccessedTime:       now,
				ExpiredTime:        -1,
				RemainQuota:        0,
				UnlimitedQuota:     true,
				ModelLimitsEnabled: false,
			})
		}

		// Batch insert
		if len(newTokens) > 0 {
			if err := tx.CreateInBatches(newTokens, 100).Error; err != nil {
				return err
			}
		}

		result = newTokens
		return nil
	})
	if err != nil {
		return nil, err
	}

	return result, nil
}

// GetKayaDefaultTokens returns all default tokens for a user.
func GetKayaDefaultTokens(userId int) ([]*Token, error) {
	var tokens []*Token
	if err := DB.Where("user_id = ? AND name LIKE ?", userId, KayaDefaultTokenNamePrefix+"%").Find(&tokens).Error; err != nil {
		return nil, err
	}
	return tokens, nil
}

// GetKayaDefaultTokensForApp returns generated default tokens for a specific app.
func GetKayaDefaultTokensForApp(userId int, appName string) ([]*Token, error) {
	groups := setting.GetDefaultGeneratedTokenGroupsForApp(appName)
	if len(groups) == 0 {
		return []*Token{}, nil
	}

	prefix := KayaDefaultTokenNamePrefix
	if strings.TrimSpace(appName) != "" {
		prefix = appDefaultTokenNamePrefix(appName)
	}

	tokenNames := make([]string, len(groups))
	for i, group := range groups {
		tokenNames[i] = fmt.Sprintf("%s (%s)", prefix, group)
	}

	var tokens []*Token
	if err := DB.Where("user_id = ? AND name IN ?", userId, tokenNames).Find(&tokens).Error; err != nil {
		return nil, err
	}
	return tokens, nil
}

func EnsureKayaDefaultTokensForApp(userId int, appName string) ([]*Token, error) {
	if _, err := EnsureKayaDefaultTokens(userId, appName); err != nil {
		return nil, err
	}
	return GetKayaDefaultTokensForApp(userId, appName)
}

// EnsureKayaDefaultToken is a convenience wrapper that creates all configured
// default tokens and returns the first one. This maintains backward compatibility
// with code that expects a single token.
func EnsureKayaDefaultToken(userId int) (*Token, error) {
	tokens, err := EnsureKayaDefaultTokens(userId)
	if err != nil {
		return nil, err
	}
	if len(tokens) > 0 {
		return tokens[0], nil
	}
	// If no new tokens were created, try to get an existing one
	existingTokens, err := GetKayaDefaultTokens(userId)
	if err != nil {
		return nil, err
	}
	if len(existingTokens) > 0 {
		return existingTokens[0], nil
	}
	return nil, nil
}
