caddyfile=/etc/caddy/conf.d/bukupan.caddy
srv_path=/srv/bukupan
domain=bukupan.localhost
upstream=127.0.0.1:8080
has_caddyfile=false

usage() {
  cat <<'USAGE'
Usage: nix run .#install -- [Caddyfile] [options]
  --srv-path PATH  Client assets (default: /srv/bukupan)
  --domain HOST    Site address (default: bukupan.localhost)
  --upstream ADDR  WebSocket server (default: 127.0.0.1:8080)
Default Caddyfile: /etc/caddy/conf.d/bukupan.caddy
USAGE
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --help|-h) usage; exit 0 ;;
    --srv-path|--domain|--upstream)
      if [[ $# -lt 2 || -z "$2" ]]; then
        printf 'Missing value for %s\n' "$1" >&2
        exit 1
      fi
      case "$1" in
        --srv-path) srv_path=$2 ;;
        --domain) domain=$2 ;;
        --upstream) upstream=$2 ;;
      esac
      shift 2
      ;;
    -*) printf 'Unknown option: %s\n' "$1" >&2; exit 1 ;;
    *)
      if "$has_caddyfile"; then usage >&2; exit 1; fi
      caddyfile=$1
      has_caddyfile=true
      shift
      ;;
  esac
done

# Values become Caddy tokens. Reject syntax metacharacters before writing files.
upstream_pattern='^([a-zA-Z0-9.-]+|\[[a-fA-F0-9:]+\]):[0-9]+$'
if [[ ! "$domain" =~ ^[a-zA-Z0-9.-]+(:[0-9]+)?$ ||
      ! "$upstream" =~ $upstream_pattern ]]; then
  printf 'Invalid domain or upstream address\n' >&2
  exit 1
fi
caddyfile=$(realpath -m -- "$caddyfile")
srv_path=$(realpath -m -- "$srv_path")
if [[ "$srv_path" == / || "$srv_path" == *[\"\$\{\}\\]* || "$srv_path" == *$'\n'* || "$srv_path" == *$'\r'* ]]; then
  printf 'Invalid asset directory\n' >&2
  exit 1
fi
case "$caddyfile" in
  "$srv_path"|"$srv_path"/*) printf 'Caddyfile must be outside the public asset directory\n' >&2; exit 1 ;;
esac

install -d -m 0755 -- "$srv_path" "$(dirname -- "$caddyfile")"
for asset in "$BUKUPAN_CLIENT_PACKAGE"/*; do
  install -m 0644 -- "$asset" "$srv_path/$(basename -- "$asset")"
done
config_tmp=$(mktemp "$(dirname -- "$caddyfile")/.bukupan.XXXXXX")
trap 'rm -f "$config_tmp"' EXIT
cat > "$config_tmp" <<CADDY
$domain {
  handle /ws {
    reverse_proxy $upstream
  }
  handle /healthz {
    reverse_proxy $upstream
  }
  handle {
    root * "$srv_path"
    header Cache-Control "no-cache"
    file_server
  }
}
CADDY
chmod 0644 "$config_tmp"
mv -f -- "$config_tmp" "$caddyfile"
printf 'Installed assets: %s\nInstalled Caddyfile: %s\n' "$srv_path" "$caddyfile"
printf 'Import this file in your main Caddyfile, then validate and reload Caddy.\n'
