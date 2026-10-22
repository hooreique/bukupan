export BUKUPAN_DEV_ADDRESS="${BUKUPAN_DEV_ADDRESS:-:3000}"
export BUKUPAN_UPSTREAM="${BUKUPAN_UPSTREAM:-127.0.0.1:3001}"
work_dir=$(mktemp -d)
server_pid=""
proxy_pid=""

cleanup() {
  trap - EXIT INT TERM
  if [[ -n "$proxy_pid" ]]; then kill "$proxy_pid" 2>/dev/null || true; fi
  if [[ -n "$server_pid" ]]; then kill "$server_pid" 2>/dev/null || true; fi
  wait 2>/dev/null || true
  rm -rf "$work_dir"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

cat > "$work_dir/Caddyfile" <<'CADDY'
{
  admin off
  auto_https off
}

{$BUKUPAN_DEV_ADDRESS} {
  handle /ws {
    reverse_proxy {$BUKUPAN_UPSTREAM}
  }
  handle /healthz {
    reverse_proxy {$BUKUPAN_UPSTREAM}
  }
  handle {
    root * "{$BUKUPAN_CLIENT_ROOT}"
    header Cache-Control "no-store"
    file_server
  }
}
CADDY

bukupan -listen "$BUKUPAN_UPSTREAM" &
server_pid=$!
XDG_CONFIG_HOME="$work_dir/config" XDG_DATA_HOME="$work_dir/data" \
  caddy run --config "$work_dir/Caddyfile" --adapter caddyfile &
proxy_pid=$!
printf '부끄판: http://localhost:3000 (listen: %s)\n' "$BUKUPAN_DEV_ADDRESS"
wait -n "$server_pid" "$proxy_pid"
