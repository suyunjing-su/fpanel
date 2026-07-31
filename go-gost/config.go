package main

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strings"
)

// Config 配置结构体
type Config struct {
	Addr        string   `json:"addr"`
	Controllers []string `json:"controllers"`
	Secret      string   `json:"secret"`
	Http        int      `json:"http"`
	Tls         int      `json:"tls"`
	Socks       int      `json:"socks"`
}

// LoadConfig 加载配置文件
func LoadConfig(configPath string) (*Config, error) {
	// 检查文件是否存在
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("配置文件不存在: %s", configPath)
	}

	// 读取文件内容
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("读取配置文件失败: %v", err)
	}

	// 解析JSON
	var config Config
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("解析配置文件失败: %v", err)
	}

	// 验证必要的配置项
	config.Controllers = normalizeControllerAddresses(config.Addr, config.Controllers)
	if len(config.Controllers) == 0 {
		return nil, fmt.Errorf("服务器地址不能为空")
	}
	config.Addr = config.Controllers[0]
	for _, address := range config.Controllers {
		u, err := url.Parse(address)
		if err != nil || u.Scheme == "" {
			return nil, fmt.Errorf("服务器地址格式错误，必须包含协议，例如 http://127.0.0.1:8534 或 https://clk.qzz.io:443: %s", address)
		}
		switch strings.ToLower(u.Scheme) {
		case "http", "https", "ws", "wss":
		default:
			return nil, fmt.Errorf("不支持的服务器地址协议: %s，仅支持 http/https/ws/wss", u.Scheme)
		}
	}

	return &config, nil
}

func normalizeControllerAddresses(addr string, controllers []string) []string {
	values := append([]string(nil), controllers...)
	if strings.TrimSpace(addr) != "" {
		values = append([]string{addr}, values...)
	}
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimRight(strings.TrimSpace(value), "/")
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}
