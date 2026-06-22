package service

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
)

func ParseRetryAfterSeconds(resp *http.Response) int {
	if resp == nil {
		return 0
	}
	raw := strings.TrimSpace(resp.Header.Get("Retry-After"))
	if raw == "" {
		return 0
	}
	seconds, err := strconv.Atoi(raw)
	if err != nil || seconds <= 0 {
		return 0
	}
	return seconds
}

func ParseRetryAfterSecondsFromBody(body []byte) int {
	if len(body) == 0 {
		return 0
	}
	var payload struct {
		RetryAfterSeconds int `json:"retry_after_seconds"`
		Error             struct {
			RetryAfterSeconds int `json:"retry_after_seconds"`
		} `json:"error"`
	}
	if err := common.Unmarshal(body, &payload); err != nil {
		return 0
	}
	if payload.RetryAfterSeconds > 0 {
		return payload.RetryAfterSeconds
	}
	return payload.Error.RetryAfterSeconds
}

func FirstRetryAfterSeconds(headerSeconds int, body []byte, fallbackSeconds int) int {
	if headerSeconds > 0 {
		return headerSeconds
	}
	if bodySeconds := ParseRetryAfterSecondsFromBody(body); bodySeconds > 0 {
		return bodySeconds
	}
	if fallbackSeconds > 0 {
		return fallbackSeconds
	}
	return 5
}
