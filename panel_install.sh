#!/bin/bash
set -Eeuo pipefail

export LC_ALL=C

RELEASE_VERSION="3.0.27-beta"
RELEASE_BASE_URL="https://github.com/suyunjing-su/fpanel/releases/download/${RELEASE_VERSION}"
CHECKSUMS_URL="${RELEASE_BASE_URL}/SHA256SUMS"
BACKEND_CONTAINER="flux-control-plane"
FRONTEND_CONTAINER="vite-frontend"
INGRESS_CONTAINER="flux-ingress"
SQLITE_VOLUME="sqlite_data"
DOCKER_CMD=()
TEMP_PATHS=()
UPDATE_ROLLBACK_ACTIVE=false
UPDATE_BACKUP_DIRECTORY=""
UPDATE_OLD_BACKEND_ID=""
UPDATE_OLD_BACKEND_REF=""
UPDATE_OLD_FRONTEND_ID=""
UPDATE_OLD_FRONTEND_REF=""
UPDATE_OLD_INGRESS_ID=""
UPDATE_OLD_INGRESS_REF=""

cleanup_temp_paths() {
  local path
  for path in "${TEMP_PATHS[@]}"; do
    [[ -n "$path" ]] && rm -rf -- "$path"
  done
}

track_temp_path() {
  TEMP_PATHS+=("$1")
}

preserve_temp_path() {
  local preserved="$1"
  local path
  local remaining=()
  for path in "${TEMP_PATHS[@]}"; do
    [[ "$path" == "$preserved" ]] || remaining+=("$path")
  done
  TEMP_PATHS=("${remaining[@]}")
}

handle_exit() {
  local status=$?
  trap - EXIT INT TERM
  if [[ "$UPDATE_ROLLBACK_ACTIVE" == "true" ]]; then
    echo "更新被中断，正在恢复旧部署" >&2
    preserve_temp_path "$UPDATE_BACKUP_DIRECTORY"
    if rollback_update "$UPDATE_BACKUP_DIRECTORY" \
      "$UPDATE_OLD_BACKEND_ID" "$UPDATE_OLD_BACKEND_REF" \
      "$UPDATE_OLD_FRONTEND_ID" "$UPDATE_OLD_FRONTEND_REF" \
      "$UPDATE_OLD_INGRESS_ID" "$UPDATE_OLD_INGRESS_REF"; then
      echo "旧版本已恢复；备份保留于 $UPDATE_BACKUP_DIRECTORY" >&2
    else
      echo "自动回滚失败；备份保留于 $UPDATE_BACKUP_DIRECTORY" >&2
    fi
  fi
  cleanup_temp_paths
  exit "$status"
}

trap handle_exit EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

check_runtime() {
  command -v curl >/dev/null 2>&1 || { echo "缺少 curl" >&2; return 1; }
  if command -v docker >/dev/null 2>&1 && docker compose version >/dev/null 2>&1; then
    DOCKER_CMD=(docker compose)
  elif command -v docker-compose >/dev/null 2>&1; then
    DOCKER_CMD=(docker-compose)
  else
    echo "需要 Docker Compose v2 或 docker-compose" >&2
    return 1
  fi
  docker info >/dev/null
}

compose() {
  "${DOCKER_CMD[@]}" "$@"
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

supports_ipv6() {
  if command -v ip >/dev/null 2>&1; then
    ip -6 addr show scope global 2>/dev/null | grep -q 'inet6'
  elif command -v ifconfig >/dev/null 2>&1; then
    ifconfig 2>/dev/null | grep -v 'fe80:' | grep -q 'inet6'
  else
    return 1
  fi
}

verify_download() {
  local checksums="$1"
  local asset="$2"
  local destination="$3"
  local expected actual
  curl --fail --location --retry 3 --proto '=https' --tlsv1.2 \
    "${RELEASE_BASE_URL}/${asset}" -o "$destination"
  expected=$(awk -v asset="$asset" '$2 == asset {print $1; exit}' "$checksums")
  actual=$(sha256_file "$destination")
  if [[ ! "$expected" =~ ^[0-9a-fA-F]{64}$ || "${actual,,}" != "${expected,,}" ]]; then
    rm -f "$destination"
    echo "${asset} SHA-256 校验失败" >&2
    return 1
  fi
}

download_candidate() {
  local directory="$1"
  local compose_asset
  if supports_ipv6; then
    compose_asset="docker-compose-v6.yml"
  else
    compose_asset="docker-compose-v4.yml"
  fi
  curl --fail --location --retry 3 --proto '=https' --tlsv1.2 \
    "$CHECKSUMS_URL" -o "$directory/SHA256SUMS"
  verify_download "$directory/SHA256SUMS" "$compose_asset" "$directory/docker-compose.yml"
  verify_download "$directory/SHA256SUMS" Caddyfile "$directory/Caddyfile"
  rm -f "$directory/SHA256SUMS"
  chmod 0644 "$directory/docker-compose.yml" "$directory/Caddyfile"
}

generate_random() {
  od -An -N32 -tx1 /dev/urandom | tr -d ' \n'
}

upsert_env() {
  local file="$1"
  local key="$2"
  local value="$3"
  if grep -q "^${key}=" "$file" 2>/dev/null; then
    sed -i "s|^${key}=.*|${key}=${value}|" "$file"
  else
    printf '%s=%s\n' "$key" "$value" >> "$file"
  fi
}

read_env() {
  local file="$1"
  local key="$2"
  grep "^${key}=" "$file" 2>/dev/null | cut -d= -f2- | tail -n 1 || true
}

validate_domain() {
  [[ "$1" =~ ^([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]{2,63}$ ]]
}

prepare_existing_env() {
  local file="$1"
  local panel_domain jwt_secret metrics_token bootstrap_username bootstrap_password
  [[ -f "$file" ]] || { echo "缺少 .env" >&2; return 1; }
  chmod 0600 "$file"
  panel_domain=$(read_env "$file" PANEL_DOMAIN)
  validate_domain "$panel_domain" || { echo ".env 中的 PANEL_DOMAIN 无效" >&2; return 1; }
  jwt_secret=$(read_env "$file" JWT_SECRET)
  metrics_token=$(read_env "$file" METRICS_TOKEN)
  bootstrap_username=$(read_env "$file" BOOTSTRAP_USERNAME)
  bootstrap_password=$(read_env "$file" BOOTSTRAP_PASSWORD)
  if [[ ${#jwt_secret} -lt 32 ]]; then
    upsert_env "$file" JWT_SECRET "$(generate_random)"
  fi
  if [[ ${#metrics_token} -lt 32 ]]; then
    upsert_env "$file" METRICS_TOKEN "$(generate_random)"
  fi
  if [[ -z "$bootstrap_username" ]]; then
    upsert_env "$file" BOOTSTRAP_USERNAME admin
  fi
  if [[ ${#bootstrap_password} -lt 12 ]]; then
    upsert_env "$file" BOOTSTRAP_PASSWORD "$(generate_random)"
  fi
}

validate_candidate() {
  local directory="$1"
  (
    cd "$directory"
    compose -f docker-compose.yml config >/dev/null
  )
  local ingress_image panel_domain
  ingress_image=$(
    cd "$directory"
    compose -f docker-compose.yml config --images | awk '$0 ~ /^caddy(:|@)/ {print; exit}'
  )
  [[ -n "$ingress_image" ]] || { echo "无法解析 Caddy 镜像" >&2; return 1; }
  panel_domain=$(read_env "$directory/.env" PANEL_DOMAIN)
  docker run --rm \
    --entrypoint caddy \
    -e "PANEL_DOMAIN=${panel_domain}" \
    -v "$directory/Caddyfile:/etc/caddy/Caddyfile:ro" \
    "$ingress_image" \
    validate --config /etc/caddy/Caddyfile >/dev/null
}

pull_candidate() {
  local directory="$1"
  (
    cd "$directory"
    compose -f docker-compose.yml pull
  )
}

container_image_id() {
  docker inspect -f '{{.Image}}' "$1"
}

container_image_ref() {
  docker inspect -f '{{.Config.Image}}' "$1"
}

container_health() {
  docker inspect -f '{{if .State.Health}}{{.State.Health.Status}}{{else}}{{.State.Status}}{{end}}' "$1" 2>/dev/null || true
}

wait_for_deployment() {
  local domain="$1"
  local attempt backend frontend ingress
  for attempt in $(seq 1 180); do
    backend=$(container_health "$BACKEND_CONTAINER")
    frontend=$(container_health "$FRONTEND_CONTAINER")
    ingress=$(container_health "$INGRESS_CONTAINER")
    if [[ "$backend" == "healthy" && "$frontend" == "healthy" && "$ingress" == "healthy" ]]; then
      if curl --fail --silent --show-error --connect-timeout 5 --max-time 10 \
        --resolve "${domain}:443:127.0.0.1" "https://${domain}/" >/dev/null; then
        return 0
      fi
    fi
    if (( attempt % 15 == 0 )); then
      printf '等待部署健康：backend=%s frontend=%s ingress=%s (%d/180)\n' \
        "${backend:-missing}" "${frontend:-missing}" "${ingress:-missing}" "$attempt"
    fi
    sleep 1
  done
  return 1
}

publish_candidate_files() {
  local directory="$1"
  install -m 0644 "$directory/docker-compose.yml" docker-compose.yml.new
  install -m 0644 "$directory/Caddyfile" Caddyfile.new
  mv -f docker-compose.yml.new docker-compose.yml
  mv -f Caddyfile.new Caddyfile
}

backup_sqlite_volume() {
  local backup_directory="$1"
  local helper_image="$2"
  mkdir -p "$backup_directory/database"
  docker run --rm --entrypoint /bin/sh \
    -v "${SQLITE_VOLUME}:/data:ro" \
    -v "$backup_directory/database:/backup" \
    "$helper_image" -c 'cp -a /data/. /backup/'
}

restore_sqlite_volume() {
  local backup_directory="$1"
  local helper_image="$2"
  docker run --rm --entrypoint /bin/sh \
    -v "${SQLITE_VOLUME}:/data" \
    -v "$backup_directory/database:/backup:ro" \
    "$helper_image" -c 'find /data -mindepth 1 -maxdepth 1 -exec rm -rf {} + && cp -a /backup/. /data/'
}

require_deployment_storage() {
  [[ -f docker-compose.yml && -f Caddyfile && -f .env ]] || {
    echo "当前目录不是完整的 Flux Panel 部署目录" >&2
    return 1
  }
  docker volume inspect "$SQLITE_VOLUME" >/dev/null
}

require_current_deployment() {
  require_deployment_storage
  docker inspect "$BACKEND_CONTAINER" "$FRONTEND_CONTAINER" "$INGRESS_CONTAINER" >/dev/null
}

install_panel() {
  check_runtime
  if [[ -e docker-compose.yml || -e Caddyfile || -e .env ]] || \
    docker inspect "$BACKEND_CONTAINER" >/dev/null 2>&1 || \
    docker volume inspect "$SQLITE_VOLUME" >/dev/null 2>&1 || \
    docker volume inspect flux_caddy_data >/dev/null 2>&1 || \
    docker volume inspect flux_caddy_config >/dev/null 2>&1; then
    echo "检测到现有部署；安装操作不会覆盖它，请使用更新" >&2
    return 1
  fi

  local panel_domain
  while true; do
    read -r -p "面板域名（DNS 必须已指向本机）: " panel_domain
    panel_domain=$(printf '%s' "$panel_domain" | tr '[:upper:]' '[:lower:]')
    validate_domain "$panel_domain" && break
    echo "请输入有效域名，例如 panel.example.com" >&2
  done

  local candidate
  candidate=$(mktemp -d "$PWD/.flux-install.XXXXXX")
  track_temp_path "$candidate"
  download_candidate "$candidate"
  umask 077
  cat > "$candidate/.env" <<EOF
JWT_SECRET=$(generate_random)
METRICS_TOKEN=$(generate_random)
BOOTSTRAP_USERNAME=admin
BOOTSTRAP_PASSWORD=$(generate_random)
PANEL_DOMAIN=$panel_domain
EOF
  chmod 0600 "$candidate/.env"
  validate_candidate "$candidate"
  pull_candidate "$candidate"
  install -m 0600 "$candidate/.env" .env.new
  mv -f .env.new .env
  publish_candidate_files "$candidate"

  if ! compose up -d || ! wait_for_deployment "$panel_domain"; then
    echo "部署健康检查失败，正在清理本次安装" >&2
    compose down --volumes --remove-orphans 2>/dev/null || true
    rm -f docker-compose.yml Caddyfile .env
    return 1
  fi

  echo "面板部署完成: https://${panel_domain}"
  echo "初始管理员账号: admin"
  echo "初始管理员密码: $(read_env .env BOOTSTRAP_PASSWORD)"
  echo "请安全保存密码并在首次登录后修改"
  rm -rf "$candidate"
}

restore_image_reference() {
  local image_id="$1"
  local image_ref="$2"
  docker image inspect "$image_id" >/dev/null
  if [[ "$image_ref" != *@sha256:* ]]; then
    docker tag "$image_id" "$image_ref"
  fi
}

rollback_update() {
  local backup_directory="$1"
  local old_backend_id="$2"
  local old_backend_ref="$3"
  local old_frontend_id="$4"
  local old_frontend_ref="$5"
  local old_ingress_id="$6"
  local old_ingress_ref="$7"

  compose down --remove-orphans 2>/dev/null || true
  install -m 0644 "$backup_directory/docker-compose.yml" docker-compose.yml.new
  install -m 0644 "$backup_directory/Caddyfile" Caddyfile.new
  install -m 0600 "$backup_directory/.env" .env.new
  mv -f docker-compose.yml.new docker-compose.yml
  mv -f Caddyfile.new Caddyfile
  mv -f .env.new .env
  restore_image_reference "$old_backend_id" "$old_backend_ref"
  restore_image_reference "$old_frontend_id" "$old_frontend_ref"
  restore_image_reference "$old_ingress_id" "$old_ingress_ref"
  restore_sqlite_volume "$backup_directory" "$old_backend_id"
  compose up -d
  wait_for_deployment "$(read_env .env PANEL_DOMAIN)"
}

update_panel() {
  check_runtime
  require_current_deployment

  local backup_directory candidate
  backup_directory=$(mktemp -d "$PWD/.flux-backup.XXXXXX")
  candidate=$(mktemp -d "$PWD/.flux-update.XXXXXX")
  track_temp_path "$backup_directory"
  track_temp_path "$candidate"
  chmod 0700 "$backup_directory" "$candidate"
  cp docker-compose.yml Caddyfile .env "$backup_directory/"
  cp .env "$candidate/.env"
  prepare_existing_env "$candidate/.env"
  download_candidate "$candidate"
  validate_candidate "$candidate"

  local old_backend_id old_backend_ref old_frontend_id old_frontend_ref old_ingress_id old_ingress_ref
  old_backend_id=$(container_image_id "$BACKEND_CONTAINER")
  old_backend_ref=$(container_image_ref "$BACKEND_CONTAINER")
  old_frontend_id=$(container_image_id "$FRONTEND_CONTAINER")
  old_frontend_ref=$(container_image_ref "$FRONTEND_CONTAINER")
  old_ingress_id=$(container_image_id "$INGRESS_CONTAINER")
  old_ingress_ref=$(container_image_ref "$INGRESS_CONTAINER")

  pull_candidate "$candidate"
  docker stop -t 30 "$BACKEND_CONTAINER"
  if ! backup_sqlite_volume "$backup_directory" "$old_backend_id"; then
    echo "SQLite 快照失败，正在恢复旧后端" >&2
    docker start "$BACKEND_CONTAINER" >/dev/null
    rm -rf "$candidate" "$backup_directory"
    return 1
  fi
  UPDATE_BACKUP_DIRECTORY="$backup_directory"
  UPDATE_OLD_BACKEND_ID="$old_backend_id"
  UPDATE_OLD_BACKEND_REF="$old_backend_ref"
  UPDATE_OLD_FRONTEND_ID="$old_frontend_id"
  UPDATE_OLD_FRONTEND_REF="$old_frontend_ref"
  UPDATE_OLD_INGRESS_ID="$old_ingress_id"
  UPDATE_OLD_INGRESS_REF="$old_ingress_ref"
  UPDATE_ROLLBACK_ACTIVE=true
  if ! compose down; then
    UPDATE_ROLLBACK_ACTIVE=false
    echo "停止旧部署失败，正在重新启动旧部署" >&2
    compose up -d
    rm -rf "$candidate" "$backup_directory"
    return 1
  fi
  install -m 0600 "$candidate/.env" .env.new
  mv -f .env.new .env
  publish_candidate_files "$candidate"

  local panel_domain
  panel_domain=$(read_env .env PANEL_DOMAIN)
  if ! compose up -d || ! wait_for_deployment "$panel_domain"; then
    echo "新版本部署失败，正在恢复配置、镜像和 SQLite 数据" >&2
    if rollback_update "$backup_directory" \
      "$old_backend_id" "$old_backend_ref" \
      "$old_frontend_id" "$old_frontend_ref" \
      "$old_ingress_id" "$old_ingress_ref"; then
      UPDATE_ROLLBACK_ACTIVE=false
      preserve_temp_path "$backup_directory"
      echo "旧版本已恢复；备份保留于 $backup_directory" >&2
    else
      UPDATE_ROLLBACK_ACTIVE=false
      preserve_temp_path "$backup_directory"
      echo "自动回滚失败；备份保留于 $backup_directory" >&2
    fi
    rm -rf "$candidate"
    return 1
  fi

  UPDATE_ROLLBACK_ACTIVE=false
  UPDATE_BACKUP_DIRECTORY=""
  UPDATE_OLD_BACKEND_ID=""
  UPDATE_OLD_BACKEND_REF=""
  UPDATE_OLD_FRONTEND_ID=""
  UPDATE_OLD_FRONTEND_REF=""
  UPDATE_OLD_INGRESS_ID=""
  UPDATE_OLD_INGRESS_REF=""
  rm -rf "$candidate" "$backup_directory"
  echo "面板更新完成"
}

uninstall_panel() {
  check_runtime
  require_deployment_storage
  read -r -p "确认卸载面板并删除 SQLite 与 Caddy 卷吗？(y/N): " confirm
  [[ "$confirm" == "y" || "$confirm" == "Y" ]] || return 0
  compose down --volumes --remove-orphans
  rm -f docker-compose.yml Caddyfile .env
  echo "面板已卸载"
}

show_menu() {
  printf '%s\n' \
    "===============================================" \
    "             Flux Panel 管理" \
    "===============================================" \
    "1. 安装面板" \
    "2. 更新面板" \
    "3. 卸载面板" \
    "4. 退出"
}

main() {
  show_menu
  read -r -p "请输入选项 (1-4): " choice
  case "$choice" in
    1) install_panel ;;
    2) update_panel ;;
    3) uninstall_panel ;;
    4) return ;;
    *) echo "无效选项" >&2; return 1 ;;
  esac
}

main
