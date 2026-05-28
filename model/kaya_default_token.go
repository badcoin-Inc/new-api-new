package model

import (
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/setting"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const KayaDefaultTokenNamePrefix = "Kaya Default Token"

// EnsureKayaDefaultTokens creates idempotent default tokens for Kaya usage.
// The tokens never expire and have unlimited quota so Kaya can use them as the
// default credentials for chat, generation, agents and tools.
// Returns the list of newly created tokens (excludes already existing ones).
func EnsureKayaDefaultTokens(userId int) ([]*Token, error) {
	if userId == 0 || !constant.GenerateDefaultToken {
		return nil, nil
	}

	groups := setting.GetDefaultGeneratedTokenGroups()
	if len(groups) == 0 {
		return nil, nil
	}

	var result []*Token
	err := DB.Transaction(func(tx *gorm.DB) error {
		// Serialize default-token creation per user without adding schema changes.
		var user User
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", userId).First(&user).Error; err != nil {
			return err
		}

		// Build token names for all groups
		tokenNames := make([]string, len(groups))
		for i, g := range groups {
			tokenNames[i] = fmt.Sprintf("%s (%s)", KayaDefaultTokenNamePrefix, g)
		}

		// Find existing tokens
		var existingTokens []Token
		if err := tx.Where("user_id = ? AND name IN ?", userId, tokenNames).Find(&existingTokens).Error; err != nil {
			return err
		}

		existingMap := make(map[string]bool)
		for _, t := range existingTokens {
			existingMap[t.Name] = true
		}

		// Build tokens to insert
		now := common.GetTimestamp()
		var newTokens []*Token
		for _, group := range groups {
			name := fmt.Sprintf("%s (%s)", KayaDefaultTokenNamePrefix, group)
			if existingMap[name] {
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
