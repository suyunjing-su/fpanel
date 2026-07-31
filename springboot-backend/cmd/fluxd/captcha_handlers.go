package main

import (
	"context"
	"net/http"
	"strings"

	"github.com/suyunjing-su/fpanel/backend/internal/auth"
	"github.com/suyunjing-su/fpanel/backend/internal/captcha"
	"github.com/suyunjing-su/fpanel/backend/internal/httpapi"
)

type captchaValidator interface {
	Runtime(context.Context) (captcha.RuntimeConfig, error)
	Validate(context.Context, captcha.Proof) error
}

type authenticator interface {
	Authenticate(context.Context, string, string) (auth.Identity, error)
}

func registerCaptchaRoutes(mux *http.ServeMux, service *captcha.Service) {
	registerCaptchaHandlers(mux, service)
}

func registerCaptchaHandlers(mux *http.ServeMux, service captchaValidator) {
	mux.HandleFunc("POST /api/v1/captcha/check", func(w http.ResponseWriter, r *http.Request) {
		runtime, err := service.Runtime(r.Context())
		if err != nil {
			httpapi.WriteJSON(w, http.StatusInternalServerError, httpapi.Failure(http.StatusInternalServerError, "验证码配置查询失败"))
			return
		}
		value := 0
		if runtime.Enabled {
			value = 1
		}
		httpapi.WriteJSON(w, http.StatusOK, httpapi.Success(value))
	})
	mux.HandleFunc("POST /api/v1/captcha/runtime", func(w http.ResponseWriter, r *http.Request) {
		runtime, err := service.Runtime(r.Context())
		if err != nil {
			httpapi.WriteJSON(w, http.StatusInternalServerError, httpapi.Failure(http.StatusInternalServerError, "验证码配置查询失败"))
			return
		}
		httpapi.WriteJSON(w, http.StatusOK, httpapi.Success(runtime))
	})
}

func registerLoginRoute(mux *http.ServeMux, service *captcha.Service, repository *auth.Repository, manager *auth.Manager) {
	registerLoginHandler(mux, service, repository, manager)
}

func registerLoginHandler(mux *http.ServeMux, service captchaValidator, repository authenticator, manager *auth.Manager) {
	mux.HandleFunc("POST /api/v1/user/login", func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Username        string `json:"username"`
			User            string `json:"user"`
			Password        string `json:"password"`
			CaptchaID       string `json:"captchaId"`
			CaptchaProvider string `json:"captchaProvider"`
			CaptchaToken    string `json:"captchaToken"`
			CaptchaPayload  string `json:"captchaPayload"`
		}
		if !httpapi.DecodeJSON(w, r, &request) {
			return
		}
		if request.Username == "" {
			request.Username = request.User
		}
		captchaToken := request.CaptchaToken
		if strings.TrimSpace(captchaToken) == "" {
			captchaToken = request.CaptchaID
		}
		if err := service.Validate(r.Context(), captcha.Proof{
			Provider: request.CaptchaProvider,
			Token:    captchaToken,
			Payload:  request.CaptchaPayload,
		}); err != nil {
			httpapi.WriteJSON(w, http.StatusUnauthorized, httpapi.Failure(http.StatusUnauthorized, "验证码校验失败"))
			return
		}
		identity, err := repository.Authenticate(r.Context(), auth.NormalizeUsername(request.Username), request.Password)
		if err != nil {
			httpapi.WriteJSON(w, http.StatusUnauthorized, httpapi.Failure(http.StatusUnauthorized, "用户名或密码错误"))
			return
		}
		token, err := manager.Issue(identity)
		if err != nil {
			httpapi.WriteJSON(w, http.StatusInternalServerError, httpapi.Failure(http.StatusInternalServerError, "登录失败"))
			return
		}
		httpapi.WriteJSON(w, http.StatusOK, httpapi.Success(map[string]any{"token": token, "role_id": identity.RoleID, "name": identity.Username}))
	})
}
