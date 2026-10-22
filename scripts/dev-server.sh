host=127.0.0.1

usage() {
  cat <<'USAGE'
Usage: nix run .#dev-server -- [options]
  --host HOST  HTTP bind host (default: 127.0.0.1)
USAGE
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --help|-h) usage; exit 0 ;;
    --host)
      if [[ $# -lt 2 || -z "$2" || "$2" == -* ]]; then
        printf 'Missing value for %s\n' "$1" >&2
        exit 1
      fi
      host=$2
      shift 2
      ;;
    --host=*) host=${1#*=}; shift ;;
    *) printf 'Unknown option: %s\n' "$1" >&2; exit 1 ;;
  esac
done

# The host becomes a Caddy token; reject whitespace and syntax metacharacters.
if [[ ! "$host" =~ ^[a-zA-Z0-9.:%_-]+$ ]]; then
  printf 'Invalid host: %s\n' "$host" >&2
  exit 1
fi
if [[ "$host" == *:* ]]; then
  host="[$host]"
fi
export BUKUPAN_DEV_HOST="$host"
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
  bind {$BUKUPAN_DEV_HOST}
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
dev_address=$BUKUPAN_DEV_ADDRESS
if [[ "$dev_address" == :* ]]; then
  dev_address="$host$dev_address"
fi
printf '부끄판: http://%s (bind: %s)\n' "$dev_address" "$host"
wait -n "$server_pid" "$proxy_pid"
