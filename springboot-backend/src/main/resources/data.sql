
INSERT OR IGNORE INTO user (id, user, pwd, role_id, exp_time, flow, in_flow, out_flow, flow_reset_time, num, created_time, updated_time, status) 
VALUES (1, 'admin_user', '3c85cdebade1c51cf64ca9f3c09d182d', 0, 2727251700000, 99999, 0, 0, 1, 99999, 1748914865000, 1754011744252, 1);


INSERT OR IGNORE INTO vite_config (id, name, value, time) 
VALUES (1, 'app_name', 'flux', 1755147963000);

INSERT OR IGNORE INTO vite_config (id, name, value, time)
VALUES (2, 'captcha_enabled', 'false', 1755147963000);

INSERT OR IGNORE INTO vite_config (id, name, value, time)
VALUES (3, 'captcha_provider', 'native', 1755147963000);

INSERT OR IGNORE INTO vite_config (id, name, value, time)
VALUES (4, 'captcha_type', 'RANDOM', 1755147963000);

INSERT OR IGNORE INTO vite_config (id, name, value, time)
VALUES (5, 'captcha_geetest_domain', 'https://gcaptcha4.geetest.com', 1755147963000);
