# 부끄판

검은 화면에 함께 그리는 흰 선. 각 선은 10초에 걸쳐 사라진다.

```sh
nix run .#dev-server
# http://localhost:3000 — 브라우저 두 개로 접속

nix build .#default  # result/bin/bukupan
nix build .#client   # result/ 아래 정적 파일
nix run .#default   # 127.0.0.1:8080, /ws 및 /healthz

# 기본 바인딩 주소는 127.0.0.1. 다른 인터페이스로 바인딩:
nix run .#default -- --host 192.168.0.6     # 192.168.0.6:8080
nix run .#dev-server -- --host 192.168.0.6  # http://192.168.0.6:3000
```

설치할 경로에 쓰기 권한이 있는 계정으로 실행:

```sh
nix run .#install -- /etc/caddy/conf.d/bukupan.caddy \
  --srv-path /srv/bukupan --domain draw.example.com
```

상위 `/etc/caddy/Caddyfile`에 `import conf.d/*.caddy`를 추가하고 Caddy 설정을 검증·리로드한다. 서버는 별도로 `nix run .#default`로 실행한다. 주소를 바꾸려면 서버에 `-- -listen 127.0.0.1:8081`, 설치에 `--upstream 127.0.0.1:8081`을 준다. 서버에 `--host`와 `-listen`을 함께 주면 `--host`가 호스트를 덮어쓰고 `-listen`의 포트를 사용한다. 설치 명령은 정적 파일과 import할 사이트 설정만 작성한다.

하나의 공유 공간, 메모리에만 보관. 인증·영속성·PWA·서비스 등록·배포 자동화는 포함하지 않는다.
