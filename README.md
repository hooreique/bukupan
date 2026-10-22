# 부끄판

검은 화면에 함께 그리는 흰 선. 각 선은 10초에 걸쳐 사라진다. 하나의 공유 공간을 메모리에 보관하며 인증과 영속성은 없다.

```sh
nix run .#dev-server  # http://localhost:3000 — 브라우저 두 개로 접속
nix build .          # result/bin/bukupan 통합 CLI
nix build .#bukupan-server
nix build .#bukupan-client  # 기존 .#client도 동일한 정적 파일 패키지
nix run . -- serve --listen-host 127.0.0.1 --listen-port 8080
```

Linux에서는 systemd 사용자 서비스로 백엔드를 관리한다. 설치는 정지·자동 시작 비활성 상태다. 설치·시작·재시작에는 linger가 필요하며 명시적으로 `--enable-linger`를 지정해야 CLI가 활성화한다.

```sh
bukupan service install --enable-linger
bukupan service start
bukupan service status
bukupan service restart  # 정지 상태에서도 시작, 자동 시작 설정 보존
bukupan service stop     # 자동 시작 비활성화와 정지
bukupan service uninstall  # 사이트는 보존; --disable-linger로만 linger 해제
```

유닛과 실행 설정은 `$XDG_CONFIG_HOME`(기본 `~/.config`), 백엔드 및 사이트별 자산 GC root는 `$XDG_STATE_HOME`(기본 `~/.local/state`) 아래에 둔다. Darwin에서는 `serve`와 사이트 명령만 지원한다. `bukupan-server`는 기존 `-listen`과 `--host` 플래그도 유지한다.

쓰기 권한이 있는 임의 경로에 Caddy 사이트 파일을 생성한다.

```sh
bukupan site install --target /path/to/sites/bukupan.caddy --domain bukupan.example.com
# HTTP 또는 HTTPS를 명시할 수도 있다: --domain http://localhost:8081
bukupan site uninstall --target /path/to/sites/bukupan.caddy
```

master Caddyfile에 `import /path/to/sites/bukupan.caddy`를 추가한 뒤 전체 설정을 검증·적용한다. CLI는 master 설정을 수정하거나 Caddy를 실행·리로드하지 않는다. 사이트 제거 후에도 import를 정리하고 전체 설정을 검증·적용한다. `/ws`, `/healthz`는 백엔드로 프록시하고 나머지는 Nix store의 정적 파일을 제공한다. Caddy 실행 계정은 자산 경로를 읽고 탐색할 수 있어야 한다.

`serve`, `service install`, `site install`의 기본 백엔드 주소는 모두 `127.0.0.1:8080`이다. `--listen-host`와 `--listen-port`(1–65535)로 변경하며 IPv6는 `--listen-host ::1`처럼 지정한다. 사이트와 서비스에 같은 주소를 직접 지정해야 한다. 포트 충돌은 서버 바인딩 오류로 종료된다.

사이트 재설치는 관리 표식이 있는 일반 파일만 원자적으로 갱신한다. 심볼릭 링크와 관리하지 않는 파일은 보호한다. 자산은 사이트별 GC root로 유지하며 갱신 성공 후 이전 root를 제거한다. 서비스 설치는 기존 설치나 파일 충돌이 있으면 실패한다.

기존 설치에서 전환할 때는 이전 사이트 설정과 `/srv/bukupan` 등의 복사 자산을 직접 확인·정리하고 새 `site install`과 `service install`을 실행한다. 이전 파일을 자동 인수하거나 삭제하지 않는다.
