package captcha

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

type memoryConfig map[string]string

func (c memoryConfig) Get(_ context.Context, key string) (string, error) {
	return c[key], nil
}

func TestValidateTokenProviders(t *testing.T) {
	providers := []struct {
		name      string
		secretKey string
		option    func(*Options, string)
	}{
		{name: "recaptcha", secretKey: "captcha_recaptcha_secret_key", option: func(options *Options, endpoint string) { options.RecaptchaEndpoint = endpoint }},
		{name: "hcaptcha", secretKey: "captcha_hcaptcha_secret_key", option: func(options *Options, endpoint string) { options.HCaptchaEndpoint = endpoint }},
		{name: "turnstile", secretKey: "captcha_turnstile_secret_key", option: func(options *Options, endpoint string) { options.TurnstileEndpoint = endpoint }},
	}
	for _, provider := range providers {
		t.Run(provider.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if err := r.ParseForm(); err != nil {
					t.Error(err)
				}
				if r.Form.Get("secret") != "provider-secret" || r.Form.Get("response") != "proof-token" {
					t.Errorf("unexpected verification form: %#v", r.Form)
				}
				_, _ = io.WriteString(w, `{"success":true}`)
			}))
			defer server.Close()

			config := memoryConfig{
				"captcha_enabled":  "true",
				"captcha_provider": provider.name,
				provider.secretKey: "provider-secret",
			}
			options := Options{HTTPClient: server.Client()}
			provider.option(&options, server.URL)
			if err := New(config, options).Validate(context.Background(), Proof{Provider: provider.name, Token: "proof-token"}); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestValidateGeeTest(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/validate" || r.URL.Query().Get("captcha_id") != "captcha-id" {
			t.Errorf("unexpected GeeTest URL: %s", r.URL.String())
		}
		if err := r.ParseForm(); err != nil {
			t.Error(err)
		}
		if r.Form.Get("lot_number") != "lot" || r.Form.Get("sign_token") != "6adf84e20aad4066ff9ba2a30bd0fe7ceb30f70d4ab88ed78937670baa0c45fa" {
			t.Errorf("unexpected GeeTest form: %#v", r.Form)
		}
		_, _ = io.WriteString(w, `{"result":"success"}`)
	}))
	defer server.Close()

	config := memoryConfig{
		"captcha_enabled":     "true",
		"captcha_provider":    "geetest",
		"captcha_geetest_id":  "captcha-id",
		"captcha_geetest_key": "captcha-key",
	}
	payload := `{"captcha_id":"captcha-id","lot_number":"lot","captcha_output":"output","pass_token":"pass","gen_time":"time"}`
	if err := New(config, Options{HTTPClient: server.Client(), GeeTestEndpoint: server.URL}).Validate(context.Background(), Proof{Provider: "geetest", Payload: payload}); err != nil {
		t.Fatal(err)
	}
}

func TestValidateFailsClosed(t *testing.T) {
	t.Run("disabled", func(t *testing.T) {
		if err := New(memoryConfig{}, Options{}).Validate(context.Background(), Proof{}); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("provider mismatch", func(t *testing.T) {
		service := New(memoryConfig{"captcha_enabled": "true", "captcha_provider": "turnstile"}, Options{})
		if !errors.Is(service.Validate(context.Background(), Proof{Provider: "recaptcha", Token: "token"}), ErrVerificationFailed) {
			t.Fatal("client-selected provider bypassed configured provider")
		}
	})
	t.Run("missing secret", func(t *testing.T) {
		service := New(memoryConfig{"captcha_enabled": "true", "captcha_provider": "turnstile"}, Options{})
		if !errors.Is(service.Validate(context.Background(), Proof{Provider: "turnstile", Token: "token"}), ErrVerificationFailed) {
			t.Fatal("missing provider secret was accepted")
		}
	})
	t.Run("upstream rejection", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = io.WriteString(w, `{"success":false}`)
		}))
		defer server.Close()
		service := New(memoryConfig{"captcha_enabled": "true", "captcha_provider": "turnstile", "captcha_turnstile_secret_key": "secret"}, Options{TurnstileEndpoint: server.URL})
		if !errors.Is(service.Validate(context.Background(), Proof{Provider: "turnstile", Token: "token"}), ErrVerificationFailed) {
			t.Fatal("upstream rejection was accepted")
		}
	})
	t.Run("malformed response", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = io.WriteString(w, `{`)
		}))
		defer server.Close()
		service := New(memoryConfig{"captcha_enabled": "true", "captcha_provider": "turnstile", "captcha_turnstile_secret_key": "secret"}, Options{TurnstileEndpoint: server.URL})
		if err := service.Validate(context.Background(), Proof{Provider: "turnstile", Token: "token"}); err == nil {
			t.Fatal("malformed upstream response was accepted")
		}
	})
	t.Run("oversized response", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = io.WriteString(w, strings.Repeat("x", maxVerificationBody+1))
		}))
		defer server.Close()
		service := New(memoryConfig{"captcha_enabled": "true", "captcha_provider": "turnstile", "captcha_turnstile_secret_key": "secret"}, Options{TurnstileEndpoint: server.URL})
		if err := service.Validate(context.Background(), Proof{Provider: "turnstile", Token: "token"}); err == nil {
			t.Fatal("oversized upstream response was accepted")
		}
	})
	t.Run("context cancellation", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			<-r.Context().Done()
		}))
		defer server.Close()
		service := New(memoryConfig{"captcha_enabled": "true", "captcha_provider": "turnstile", "captcha_turnstile_secret_key": "secret"}, Options{TurnstileEndpoint: server.URL, HTTPClient: &http.Client{Timeout: time.Second}})
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if err := service.Validate(ctx, Proof{Provider: "turnstile", Token: "token"}); err == nil {
			t.Fatal("cancelled verification was accepted")
		}
	})
}

func TestRuntimeNeverExposesSecrets(t *testing.T) {
	config := memoryConfig{
		"captcha_enabled":              "true",
		"captcha_provider":             "turnstile",
		"captcha_geetest_key":          "geetest-secret",
		"captcha_recaptcha_secret_key": "recaptcha-secret",
		"captcha_hcaptcha_secret_key":  "hcaptcha-secret",
		"captcha_turnstile_site_key":   "public-site-key",
		"captcha_turnstile_secret_key": "turnstile-secret",
	}
	runtime, err := New(config, Options{}).Runtime(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if runtime.TurnstileSiteKey != "public-site-key" || !runtime.TurnstileSecretKeyConfigured || !runtime.GeeTestKeyConfigured || !runtime.RecaptchaSecretKeyConfigured || !runtime.HCaptchaSecretKeyConfigured {
		t.Fatalf("runtime configuration is incomplete: %#v", runtime)
	}
}

func TestGeeTestEndpointValidation(t *testing.T) {
	for _, domain := range []string{"http://example.com", "https://user@example.com", "https://example.com/path", "https://example.com?query=1"} {
		if endpoint, err := geetestEndpoint(domain, "id"); err == nil {
			t.Fatalf("unsafe GeeTest domain was accepted: %s", endpoint)
		}
	}
	endpoint, err := geetestEndpoint("https://gcaptcha4.geetest.com/", "id with spaces")
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Query().Get("captcha_id") != "id with spaces" {
		t.Fatalf("captcha ID was not encoded: %q, %v", endpoint, err)
	}
}
