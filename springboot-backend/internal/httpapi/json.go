package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
)

var ErrInvalidRequest = errors.New("invalid request")

func DecodeJSON(w http.ResponseWriter, r *http.Request, target any) bool {
	defer r.Body.Close()
	decoder := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		WriteJSON(w, http.StatusBadRequest, Failure(http.StatusBadRequest, "请求参数错误"))
		return false
	}
	return true
}

func Method(method string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != method {
			w.Header().Set("Allow", method)
			WriteJSON(w, http.StatusMethodNotAllowed, Failure(http.StatusMethodNotAllowed, "请求方法不允许"))
			return
		}
		next.ServeHTTP(w, r)
	})
}

func ErrorResponse(err error) APIResponse {
	if errors.Is(err, ErrInvalidRequest) {
		return Failure(http.StatusBadRequest, "请求参数错误")
	}
	return Failure(http.StatusInternalServerError, fmt.Sprintf("服务器内部错误: %v", err))
}
