package batchdelete

import (
	"database/sql"
	"errors"
	"testing"
)

func TestExecuteDeduplicatesAndRetainsFailures(t *testing.T) {
	calls := make(map[int64]int)
	result := Execute([]int64{1, 2, 1, -1, 3}, func(id int64) (string, error) {
		calls[id]++
		switch id {
		case 1:
			return "first", nil
		case 2:
			return "second", errors.New("FOREIGN KEY constraint failed")
		case -1:
			return "", errors.New("resource id must be positive")
		default:
			return "", sql.ErrNoRows
		}
	})
	if result.TotalCount != 4 || result.SuccessCount != 1 || result.FailureCount != 3 {
		t.Fatalf("unexpected counts: %#v", result)
	}
	if calls[1] != 1 {
		t.Fatalf("duplicate id executed %d times", calls[1])
	}
	if result.Failures[0].Name != "second" || result.Failures[0].Reason != "资源正在被其他配置使用" {
		t.Fatalf("unexpected referenced failure: %#v", result.Failures[0])
	}
	if result.Failures[1].Reason != "资源 ID 无效" || result.Failures[2].Name != "ID 3" || result.Failures[2].Reason != "资源不存在或无权访问" {
		t.Fatalf("unexpected normalized failures: %#v", result.Failures)
	}
}
