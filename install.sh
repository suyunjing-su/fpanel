#!/bin/bash
set -Eeuo pipefail

RELEASE_VERSION="3.0.30-beta"
RELEASE_BASE_URL="https://github.com/suyunjing-su/fpanel/releases/download/${RELEASE_VERSION}"
CHECKSUMS_URL="${RELEASE_BASE_URL}/SHA256SUMS"
BINARY_DIR="/usr/local/lib/flux-agent"
BINARY_PATH="${BINARY_DIR}/flux-agent"
ROLLBACK_PATH="${BINARY_DIR}/flux-agent.rollback"
STATE_DIR="/var/lib/flux-agent"
CONFIG_PATH="${STATE_DIR}/config.json"
GOST_CONFIG_PATH="${STATE_DIR}/gost.json"
SERVICE_FILE="/etc/systemd/system/flux-agent.service"
AGENT_USER="flux-agent"
SERVER_ADDR=""
SECRET=""

get_architecture() {
  case "$(uname -m)" in
    x86_64) printf '%s\n' amd64 ;;
    aarch64|arm64) printf '%s\n' arm64 ;;
    *) printf '不支持的系统架构: %s\n' "$(uname -m)" >&2; return 1 ;;
  esac
}

sha256_file() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{print $1}'
  elif command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "$1" | awk '{print $1}'
  else
    echo "缺少 sha256sum 或 shasum" >&2
    return 1
  fi
}

require_runtime() {
  if [[ $EUID -ne 0 ]]; then
    echo "请以 root 用户运行安装器" >&2
    return 1
  fi
  if [[ "$(uname -s)" != "Linux" ]] || ! command -v systemctl >/dev/null 2>&1; then
    echo "仅支持使用 systemd 的 Linux 系统" >&2
    return 1
  fi
  command -v curl >/dev/null 2>&1 || { echo "缺少 curl" >&2; return 1; }
  sha256_file /dev/null >/dev/null
}

validate_config() {
  case "$SERVER_ADDR" in
    https://*|wss://*) ;;
    *) echo "服务器地址必须以 https:// 或 wss:// 开头" >&2; return 1 ;;
  esac
  if [[ "$SERVER_ADDR" =~ [[:space:]\"\\] ]]; then
    echo "服务器地址包含不支持的字符" >&2
    return 1
  fi
  if [[ -z "$SECRET" || "$SECRET" =~ [^A-Za-z0-9._~-] ]]; then
    echo "密钥只能包含字母、数字、点、下划线、波浪线和连字符" >&2
    return 1
  fi
}

download_verified_binary() {
  local output="$1"
  local architecture asset checksums expected actual
  architecture=$(get_architecture)
  asset="gost-${architecture}"
  checksums="${output}.SHA256SUMS"
  rm -f "$output" "$checksums"
  curl --fail --location --retry 3 --proto '=https' --tlsv1.2 \
    "$CHECKSUMS_URL" -o "$checksums"
  curl --fail --location --retry 3 --proto '=https' --tlsv1.2 \
    "${RELEASE_BASE_URL}/${asset}" -o "$output"
  expected=$(awk -v asset="$asset" '$2 == asset {print $1; exit}' "$checksums")
  actual=$(sha256_file "$output")
  rm -f "$checksums"
  if [[ ! "$expected" =~ ^[0-9a-fA-F]{64}$ || "${actual,,}" != "${expected,,}" ]]; then
    rm -f "$output"
    echo "flux-agent SHA-256 校验失败" >&2
    return 1
  fi
  chmod 0755 "$output"
  chown root:root "$output"
}

ensure_agent_user() {
  if ! id "$AGENT_USER" >/dev/null 2>&1; then
    if command -v useradd >/dev/null 2>&1; then
      useradd --system --home-dir "$STATE_DIR" --shell /usr/sbin/nologin "$AGENT_USER"
    elif command -v adduser >/dev/null 2>&1; then
      adduser -S -H -h "$STATE_DIR" -s /sbin/nologin "$AGENT_USER"
    else
      echo "系统缺少 useradd 或 adduser" >&2
      return 1
    fi
  fi
  install -d -m 0755 -o root -g root "$BINARY_DIR"
  install -d -m 0750 -o "$AGENT_USER" -g "$AGENT_USER" "$STATE_DIR"
}

write_service() {
  local candidate="${SERVICE_FILE}.new"
  cat > "$candidate" <<EOF
[Unit]
Description=Flux Agent
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=$AGENT_USER
Group=$AGENT_USER
WorkingDirectory=$STATE_DIR
ExecStart=$BINARY_PATH
Restart=on-failure
RestartSec=5s
UMask=0077
NoNewPrivileges=true
PrivateTmp=true
PrivateDevices=true
ProtectSystem=strict
ProtectHome=true
ProtectKernelTunables=true
ProtectKernelModules=true
ProtectControlGroups=true
ProtectClock=true
ProtectHostname=true
ProtectProc=invisible
ProcSubset=pid
RestrictNamespaces=true
RestrictRealtime=true
RestrictSUIDSGID=true
LockPersonality=true
MemoryDenyWriteExecute=true
CapabilityBoundingSet=CAP_NET_BIND_SERVICE CAP_NET_RAW
AmbientCapabilities=CAP_NET_BIND_SERVICE CAP_NET_RAW
RestrictAddressFamilies=AF_UNIX AF_INET AF_INET6
ReadWritePaths=$STATE_DIR

[Install]
WantedBy=multi-user.target
EOF
  chmod 0644 "$candidate"
  chown root:root "$candidate"
  mv -f "$candidate" "$SERVICE_FILE"
  systemctl daemon-reload
}

stage_binary() {
  local candidate="${BINARY_PATH}.new"
  download_verified_binary "$candidate"
  "$candidate" -V
}

activate_candidate() {
  local candidate="${BINARY_PATH}.new"
  rm -f "$ROLLBACK_PATH"
  if [[ -f "$BINARY_PATH" ]]; then
    ln "$BINARY_PATH" "$ROLLBACK_PATH"
  fi
  mv -f "$candidate" "$BINARY_PATH"
  sync "$BINARY_PATH" 2>/dev/null || sync
}

rollback_binary() {
  systemctl stop flux-agent.service 2>/dev/null || true
  if [[ -f "$ROLLBACK_PATH" ]]; then
    mv -f "$ROLLBACK_PATH" "$BINARY_PATH"
    sync "$BINARY_PATH" 2>/dev/null || sync
  else
    rm -f "$BINARY_PATH"
  fi
}

start_and_verify() {
  systemctl enable flux-agent.service
  systemctl restart flux-agent.service
  sleep 3
  systemctl is-active --quiet flux-agent.service
}

get_config_params() {
  if [[ -z "$SERVER_ADDR" ]]; then
    read -r -p "服务器地址（https:// 或 wss://）: " SERVER_ADDR
  fi
  if [[ -z "$SECRET" ]]; then
    read -r -p "密钥: " SECRET
  fi
  validate_config
}

install_agent() {
  require_runtime
  if [[ -e "$BINARY_PATH" || -e "$SERVICE_FILE" ]]; then
    echo "flux-agent 已安装；请使用更新操作" >&2
    return 1
  fi
  get_config_params
  ensure_agent_user
  stage_binary

  umask 077
  cat > "${CONFIG_PATH}.new" <<EOF
{
  "addr": "$SERVER_ADDR",
  "secret": "$SECRET"
}
EOF
  chown root:"$AGENT_USER" "${CONFIG_PATH}.new"
  chmod 0640 "${CONFIG_PATH}.new"
  mv -f "${CONFIG_PATH}.new" "$CONFIG_PATH"
  if [[ ! -f "$GOST_CONFIG_PATH" ]]; then
    printf '{}\n' > "$GOST_CONFIG_PATH"
  fi
  chown "$AGENT_USER:$AGENT_USER" "$GOST_CONFIG_PATH"
  chmod 0600 "$GOST_CONFIG_PATH"

  write_service
  activate_candidate
  if ! start_and_verify; then
    echo "新版本启动失败，正在回滚" >&2
    rollback_binary
    systemctl disable flux-agent.service 2>/dev/null || true
    rm -f "$SERVICE_FILE"
    systemctl daemon-reload
    return 1
  fi
  rm -f "$ROLLBACK_PATH"
  echo "flux-agent 安装完成"
  echo "配置目录: $STATE_DIR"
}

update_agent() {
  require_runtime
  if [[ ! -x "$BINARY_PATH" || ! -f "$SERVICE_FILE" || ! -f "$CONFIG_PATH" ]]; then
    echo "未检测到当前架构的 flux-agent 安装" >&2
    return 1
  fi
  ensure_agent_user
  stage_binary
  local service_backup="${SERVICE_FILE}.rollback"
  rm -f "$service_backup"
  cp -a "$SERVICE_FILE" "$service_backup"
  write_service
  systemctl stop flux-agent.service
  activate_candidate
  if ! start_and_verify; then
    echo "新版本启动失败，正在回滚" >&2
    rollback_binary
    mv -f "$service_backup" "$SERVICE_FILE"
    systemctl daemon-reload
    systemctl start flux-agent.service
    systemctl is-active --quiet flux-agent.service || echo "旧版本服务恢复失败" >&2
    return 1
  fi
  rm -f "$ROLLBACK_PATH" "$service_backup"
  echo "flux-agent 更新完成"
}

uninstall_agent() {
  require_runtime
  read -r -p "确认卸载 flux-agent 并删除其配置与状态吗？(y/N): " confirm
  [[ "$confirm" == "y" || "$confirm" == "Y" ]] || return 0
  systemctl disable --now flux-agent.service 2>/dev/null || true
  rm -f "$SERVICE_FILE"
  systemctl daemon-reload
  rm -rf "$BINARY_DIR" "$STATE_DIR"
  if id "$AGENT_USER" >/dev/null 2>&1; then
    if command -v userdel >/dev/null 2>&1; then
      userdel "$AGENT_USER"
    elif command -v deluser >/dev/null 2>&1; then
      deluser "$AGENT_USER"
    fi
  fi
  echo "flux-agent 已卸载"
}

show_menu() {
  printf '%s\n' \
    "===============================================" \
    "              Flux Agent 管理" \
    "===============================================" \
    "1. 安装" \
    "2. 更新" \
    "3. 卸载" \
    "4. 退出"
}

while getopts "a:s:" opt; do
  case "$opt" in
    a) SERVER_ADDR="$OPTARG" ;;
    s) SECRET="$OPTARG" ;;
    *) exit 1 ;;
  esac
done

main() {
  if [[ -n "$SERVER_ADDR" || -n "$SECRET" ]]; then
    install_agent
    return
  fi
  show_menu
  read -r -p "请输入选项 (1-4): " choice
  case "$choice" in
    1) install_agent ;;
    2) update_agent ;;
    3) uninstall_agent ;;
    4) return ;;
    *) echo "无效选项" >&2; return 1 ;;
  esac
}

main
