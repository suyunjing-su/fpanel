INSERT OR IGNORE INTO site_config(key, value, secret, updated_at) VALUES
    ('app_logo', '', 0, 0),
    ('login_page_description', '', 0, 0),
    ('ip', '', 0, 0),
    ('protocol_type', 'http', 0, 0),
    ('captcha_provider', 'geetest', 0, 0),
    ('captcha_geetest_id', '', 0, 0),
    ('captcha_geetest_key', '', 1, 0),
    ('captcha_recaptcha_site_key', '', 0, 0),
    ('captcha_recaptcha_secret_key', '', 1, 0),
    ('captcha_hcaptcha_site_key', '', 0, 0),
    ('captcha_hcaptcha_secret_key', '', 1, 0),
    ('captcha_turnstile_site_key', '', 0, 0),
    ('captcha_turnstile_secret_key', '', 1, 0);

UPDATE site_config
SET value = 'geetest', updated_at = CAST(strftime('%s','now') AS INTEGER) * 1000
WHERE key = 'captcha_provider' AND LOWER(TRIM(value)) = 'native';

DELETE FROM site_config WHERE key IN ('captcha_type', 'captcha_geetest_domain');

UPDATE site_config
SET secret = 1
WHERE key IN (
    'captcha_geetest_key',
    'captcha_recaptcha_secret_key',
    'captcha_hcaptcha_secret_key',
    'captcha_turnstile_secret_key'
);
