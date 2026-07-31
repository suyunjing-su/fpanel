package captcha

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	defaultGeeTestDomain = "https://gcaptcha4.geetest.com"
	recaptchaVerifyURL   = "https://www.google.com/recaptcha/api/siteverify"
	hcaptchaVerifyURL    = "https://hcaptcha.com/siteverify"
	turnstileVerifyURL   = "https://challenges.cloudflare.com/turnstile/v0/siteverify"
	verificationTimeout  = 5 * time.Second
	maxVerificationBody  = 64 << 10
)

var ErrVerificationFailed = errors.New("captcha verification failed")

type ConfigStore interface {
	Get(context.Context, string) (string, error)
}

type Options struct {
	HTTPClient        *http.Client
	GeeTestEndpoint   string
	RecaptchaEndpoint string
	HCaptchaEndpoint  string
	TurnstileEndpoint string
}

type Service struct {
	config            ConfigStore
	client            *http.Client
	geetestEndpoint   string
	recaptchaEndpoint string
	hcaptchaEndpoint  string
	turnstileEndpoint string
}

type RuntimeConfig struct {
	Enabled                      bool   `json:"enabled"`
	Provider                     string `json:"provider"`
	GeeTestCaptchaID             string `json:"geetestCaptchaId,omitempty"`
	GeeTestKeyConfigured         bool   `json:"geetestKeyConfigured"`
	RecaptchaSiteKey             string `json:"recaptchaSiteKey,omitempty"`
	RecaptchaSecretKeyConfigured bool   `json:"recaptchaSecretKeyConfigured"`
	HCaptchaSiteKey              string `json:"hcaptchaSiteKey,omitempty"`
	HCaptchaSecretKeyConfigured  bool   `json:"hcaptchaSecretKeyConfigured"`
	TurnstileSiteKey             string `json:"turnstileSiteKey,omitempty"`
	TurnstileSecretKeyConfigured bool   `json:"turnstileSecretKeyConfigured"`
}

type Proof struct {
	Provider string
	Token    string
	Payload  string
}

type providerConfig struct {
	enabled            bool
	provider           string
	geetestID          string
	geetestKey         string
	recaptchaSiteKey   string
	recaptchaSecretKey string
	hcaptchaSiteKey    string
	hcaptchaSecretKey  string
	turnstileSiteKey   string
	turnstileSecretKey string
}

type geetestPayload struct {
	LotNumber     string `json:"lot_number"`
	CaptchaOutput string `json:"captcha_output"`
	PassToken     string `json:"pass_token"`
	GenTime       string `json:"gen_time"`
}

func New(config ConfigStore, options Options) *Service {
	client := options.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: verificationTimeout}
	}
	geetestEndpoint := strings.TrimSpace(options.GeeTestEndpoint)
	if geetestEndpoint == "" {
		geetestEndpoint = defaultGeeTestDomain
	}
	recaptchaEndpoint := strings.TrimSpace(options.RecaptchaEndpoint)
	if recaptchaEndpoint == "" {
		recaptchaEndpoint = recaptchaVerifyURL
	}
	hcaptchaEndpoint := strings.TrimSpace(options.HCaptchaEndpoint)
	if hcaptchaEndpoint == "" {
		hcaptchaEndpoint = hcaptchaVerifyURL
	}
	turnstileEndpoint := strings.TrimSpace(options.TurnstileEndpoint)
	if turnstileEndpoint == "" {
		turnstileEndpoint = turnstileVerifyURL
	}
	return &Service{
		config:            config,
		client:            client,
		geetestEndpoint:   geetestEndpoint,
		recaptchaEndpoint: recaptchaEndpoint,
		hcaptchaEndpoint:  hcaptchaEndpoint,
		turnstileEndpoint: turnstileEndpoint,
	}
}

func (s *Service) Runtime(ctx context.Context) (RuntimeConfig, error) {
	config, err := s.loadConfig(ctx)
	if err != nil {
		return RuntimeConfig{}, err
	}
	return RuntimeConfig{
		Enabled:                      config.enabled,
		Provider:                     config.provider,
		GeeTestCaptchaID:             config.geetestID,
		GeeTestKeyConfigured:         config.geetestKey != "",
		RecaptchaSiteKey:             config.recaptchaSiteKey,
		RecaptchaSecretKeyConfigured: config.recaptchaSecretKey != "",
		HCaptchaSiteKey:              config.hcaptchaSiteKey,
		HCaptchaSecretKeyConfigured:  config.hcaptchaSecretKey != "",
		TurnstileSiteKey:             config.turnstileSiteKey,
		TurnstileSecretKeyConfigured: config.turnstileSecretKey != "",
	}, nil
}

func (s *Service) Validate(ctx context.Context, proof Proof) error {
	config, err := s.loadConfig(ctx)
	if err != nil {
		return err
	}
	if !config.enabled {
		return nil
	}
	if strings.ToLower(strings.TrimSpace(proof.Provider)) != config.provider {
		return ErrVerificationFailed
	}

	switch config.provider {
	case "geetest":
		return s.verifyGeeTest(ctx, config, proof.Payload)
	case "recaptcha":
		return s.verifyToken(ctx, s.recaptchaEndpoint, config.recaptchaSecretKey, proof.Token)
	case "hcaptcha":
		return s.verifyToken(ctx, s.hcaptchaEndpoint, config.hcaptchaSecretKey, proof.Token)
	case "turnstile":
		return s.verifyToken(ctx, s.turnstileEndpoint, config.turnstileSecretKey, proof.Token)
	default:
		return fmt.Errorf("unsupported captcha provider %q", config.provider)
	}
}

func (s *Service) loadConfig(ctx context.Context) (providerConfig, error) {
	keys := []string{
		"captcha_enabled",
		"captcha_provider",
		"captcha_geetest_id",
		"captcha_geetest_key",
		"captcha_recaptcha_site_key",
		"captcha_recaptcha_secret_key",
		"captcha_hcaptcha_site_key",
		"captcha_hcaptcha_secret_key",
		"captcha_turnstile_site_key",
		"captcha_turnstile_secret_key",
	}
	values := make(map[string]string, len(keys))
	for _, key := range keys {
		value, err := s.config.Get(ctx, key)
		if err != nil {
			return providerConfig{}, fmt.Errorf("load captcha config %q: %w", key, err)
		}
		values[key] = strings.TrimSpace(value)
	}
	provider := strings.ToLower(values["captcha_provider"])
	if provider == "" {
		provider = "geetest"
	}
	return providerConfig{
		enabled:            parseEnabled(values["captcha_enabled"]),
		provider:           provider,
		geetestID:          values["captcha_geetest_id"],
		geetestKey:         values["captcha_geetest_key"],
		recaptchaSiteKey:   values["captcha_recaptcha_site_key"],
		recaptchaSecretKey: values["captcha_recaptcha_secret_key"],
		hcaptchaSiteKey:    values["captcha_hcaptcha_site_key"],
		hcaptchaSecretKey:  values["captcha_hcaptcha_secret_key"],
		turnstileSiteKey:   values["captcha_turnstile_site_key"],
		turnstileSecretKey: values["captcha_turnstile_secret_key"],
	}, nil
}

func (s *Service) verifyGeeTest(ctx context.Context, config providerConfig, rawPayload string) error {
	if config.geetestID == "" || config.geetestKey == "" || strings.TrimSpace(rawPayload) == "" {
		return ErrVerificationFailed
	}
	var payload geetestPayload
	decoder := json.NewDecoder(strings.NewReader(rawPayload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&payload); err != nil || strings.TrimSpace(payload.LotNumber) == "" || strings.TrimSpace(payload.CaptchaOutput) == "" || strings.TrimSpace(payload.PassToken) == "" || strings.TrimSpace(payload.GenTime) == "" {
		return ErrVerificationFailed
	}
	endpoint, err := geetestEndpoint(s.geetestEndpoint, config.geetestID)
	if err != nil {
		return err
	}
	mac := hmac.New(sha256.New, []byte(config.geetestKey))
	_, _ = mac.Write([]byte(payload.LotNumber))
	form := url.Values{
		"lot_number":     {payload.LotNumber},
		"captcha_output": {payload.CaptchaOutput},
		"pass_token":     {payload.PassToken},
		"gen_time":       {payload.GenTime},
		"sign_token":     {hex.EncodeToString(mac.Sum(nil))},
	}
	var response struct {
		Result string `json:"result"`
	}
	if err := s.postForm(ctx, endpoint, form, &response); err != nil {
		return err
	}
	if !strings.EqualFold(response.Result, "success") {
		return ErrVerificationFailed
	}
	return nil
}

func (s *Service) verifyToken(ctx context.Context, endpoint, secret, token string) error {
	if strings.TrimSpace(secret) == "" || strings.TrimSpace(token) == "" {
		return ErrVerificationFailed
	}
	var response struct {
		Success bool `json:"success"`
	}
	if err := s.postForm(ctx, endpoint, url.Values{"secret": {secret}, "response": {token}}, &response); err != nil {
		return err
	}
	if !response.Success {
		return ErrVerificationFailed
	}
	return nil
}

func (s *Service) postForm(ctx context.Context, endpoint string, form url.Values, target any) error {
	requestCtx, cancel := context.WithTimeout(ctx, verificationTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext(requestCtx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return fmt.Errorf("create captcha verification request: %w", err)
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, err := s.client.Do(request)
	if err != nil {
		return fmt.Errorf("request captcha verification: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("captcha verification returned HTTP %d", response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxVerificationBody+1))
	if err != nil {
		return fmt.Errorf("read captcha verification response: %w", err)
	}
	if len(body) > maxVerificationBody {
		return errors.New("captcha verification response is too large")
	}
	if err := json.Unmarshal(body, target); err != nil {
		return fmt.Errorf("decode captcha verification response: %w", err)
	}
	return nil
}

func geetestEndpoint(domain, captchaID string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(domain))
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.Path != "" && parsed.Path != "/") {
		return "", errors.New("invalid GeeTest verification domain")
	}
	parsed.Path = "/validate"
	parsed.RawQuery = url.Values{"captcha_id": {captchaID}}.Encode()
	return parsed.String(), nil
}

func parseEnabled(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}
