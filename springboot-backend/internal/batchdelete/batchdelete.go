package batchdelete

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

type Failure struct {
	ID     int64  `json:"id"`
	Name   string `json:"name"`
	Reason string `json:"reason"`
}

type Result struct {
	TotalCount   int       `json:"totalCount"`
	SuccessCount int       `json:"successCount"`
	FailureCount int       `json:"failureCount"`
	Failures     []Failure `json:"failures"`
}

func Execute(ids []int64, remove func(int64) (string, error)) Result {
	result := Result{Failures: make([]Failure, 0)}
	seen := make(map[int64]struct{}, len(ids))
	for _, id := range ids {
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		result.TotalCount++
		name, err := remove(id)
		if err == nil {
			result.SuccessCount++
			continue
		}
		if strings.TrimSpace(name) == "" {
			name = fmt.Sprintf("ID %d", id)
		}
		result.Failures = append(result.Failures, Failure{ID: id, Name: name, Reason: PublicReason(err)})
	}
	result.FailureCount = len(result.Failures)
	return result
}

func PublicReason(err error) string {
	if errors.Is(err, sql.ErrNoRows) {
		return "资源不存在或无权访问"
	}
	message := strings.ToLower(err.Error())
	switch {
	case strings.Contains(message, "positive"):
		return "资源 ID 无效"
	case strings.Contains(message, "access denied"):
		return "资源不存在或无权访问"
	case strings.Contains(message, "assigned") || strings.Contains(message, "foreign key") || strings.Contains(message, "constraint failed"):
		return "资源正在被其他配置使用"
	default:
		return "删除失败，请稍后重试"
	}
}
