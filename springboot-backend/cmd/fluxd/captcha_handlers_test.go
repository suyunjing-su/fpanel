package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/suyunjing-su/fpanel/backend/internal/auth"
	"github.com/suyunjing-su/fpanel/backend/internal/captcha"
)

type captchaHandlerStub struct {
	runtime captcha.RuntimeConfig
	err     error
	proof   captcha.Proof
}

func (s *captchaHandlerStub) Runtime(context.Context) (captcha.RuntimeConfig, error) {
	return s.runtime, s.err
}

func (s *captchaHandlerStub) Validate(_ context.Context, proof captcha.Proof) error {
	s.proof = proof
	return s.err
}

type authHandlerStub struct {
	identity auth.Identity
	called   bool
}

func (s *authHandlerStub) Authenticate(_ context.Context, username, password string) (auth.Identity, error) {
	s.called = true
	if username != "admin" || password != "password" {
		return auth.Identity{}, errors.New("invalid credentials")
	}
	return s.identity, nil
}

func TestLoginHandlerAcceptsCompleteCaptchaDTOWhenDisabled(t *testing.T) {
	captchaStub := &captchaHandlerStub{}
	authStub := &authHandlerStub{identity: auth.Identity{UserID: 1, Username: "admin", Role: "admin", RoleID: 0, TokenVersion: 1}}
	mux := http.NewServeMux()
	registerLoginHandler(mux, captchaStub, authStub, auth.New("test-secret-with-sufficient-entropy", time.Hour))

	response := postJSON(mux, "/api/v1/user/login", `{"username":"admin","password":"password","captchaId":"","captchaProvider":"","captchaToken":"","captchaPayload":""}`)
	if response.Code != http.StatusOK {
		t.Fatalf("login status = %d, body=%s", response.Code, response.Body.String())
	}
	if !authStub.called {
		t.Fatal("credentials were not authenticated")
	}
	var body struct {
		Code int `json:"code"`
		Data struct {
			Token string `json:"token"`
		} `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil || body.Code != 0 || body.Data.Token == "" {
		t.Fatalf("invalid login response: %s, %v", response.Body.String(), err)
	}
}

func TestLoginHandlerRejectsCaptchaBeforeCredentials(t *testing.T) {
	captchaStub := &captchaHandlerStub{err: captcha.ErrVerificationFailed}
	authStub := &authHandlerStub{}
	mux := http.NewServeMux()
	registerLoginHandler(mux, captchaStub, authStub, auth.New("test-secret-with-sufficient-entropy", time.Hour))

	response := postJSON(mux, "/api/v1/user/login", `{"username":"admin","password":"password","captchaId":"legacy","captchaProvider":"recaptcha","captchaToken":"token","captchaPayload":"payload"}`)
	if response.Code != http.StatusUnauthorized || authStub.called {
		t.Fatalf("captcha rejection status=%d authenticated=%v body=%s", response.Code, authStub.called, response.Body.String())
	}
	if captchaStub.proof.Provider != "recaptcha" || captchaStub.proof.Token != "token" || captchaStub.proof.Payload != "payload" {
		t.Fatalf("captcha proof was not preserved: %#v", captchaStub.proof)
	}
}

func TestCaptchaRuntimeResponseContainsOnlyPublicState(t *testing.T) {
	stub := &captchaHandlerStub{runtime: captcha.RuntimeConfig{
		Enabled:                      true,
		Provider:                     "turnstile",
		TurnstileSiteKey:             "public-key",
		TurnstileSecretKeyConfigured: true,
	}}
	mux := http.NewServeMux()
	registerCaptchaHandlers(mux, stub)

	response := postJSON(mux, "/api/v1/captcha/runtime", `{}`)
	if response.Code != http.StatusOK || bytes.Contains(response.Body.Bytes(), []byte("secret-value")) {
		t.Fatalf("invalid runtime response: status=%d body=%s", response.Code, response.Body.String())
	}
	var body struct {
		Data captcha.RuntimeConfig `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil || body.Data.TurnstileSiteKey != "public-key" || !body.Data.TurnstileSecretKeyConfigured {
		t.Fatalf("runtime response is incomplete: %s, %v", response.Body.String(), err)
	}
}

func postJSON(handler http.Handler, path, body string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodPost, path, bytes.NewBufferString(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}
