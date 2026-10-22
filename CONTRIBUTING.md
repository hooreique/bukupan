# 개발

브라우저는 HTML Canvas / Pointer Events / WebSocket과 순수 JavaScript, 서버는 Go 표준 라이브러리만 사용한다. npm이나 별도 모듈 설치는 없다.

```sh
nix develop
go test ./...
go test -race ./...
gofmt -w server/*.go
nixfmt flake.nix
```

`nix run .#dev-server`는 Go 서버(`127.0.0.1:3001`)와 Caddy(`127.0.0.1:3000`)를 함께 실행하고 종료 시 둘 다 정리한다. `nix run .#dev-server -- --host 192.168.0.6`으로 Caddy의 바인딩 인터페이스를 변경할 수 있다. 소스의 클라이언트를 바로 반영하려면 `BUKUPAN_CLIENT_ROOT="$PWD/client" nix run .#dev-server`로 실행한다. `BUKUPAN_DEV_ADDRESS`(기본 `:3000`)로 Caddy의 사이트 주소·포트를, `BUKUPAN_UPSTREAM`(기본 `127.0.0.1:3001`)으로 Go 서버의 주소를 변경할 수 있다. Go 변경 후에는 재시작한다.

flake는 `x86_64-linux`, `aarch64-linux`, `aarch64-darwin`만 지원한다. 평가 전에 새 파일을 `git add`해야 한다. Raspberry Pi에서는 해당 저장소에서 동일한 빌드·실행 명령을 쓴다.

## 공유와 수명

`/ws`에 접속하면 서버 시각(`now`), 수명(`lifetime`, 밀리초), 아직 살아 있는 `segments`를 담은 `snapshot`을 받는다. 그다음 `{type: "draw", segments: [...]}`를 주고받는다. 선분은 `{id, x1, y1, x2, y2, at}`이며 좌표는 뷰포트의 0–1 비율이다. 화면 비율이 다르면 그림도 그 비율에 맞춰 늘어난다. 선 두께는 3 CSS px다.

클라이언트는 즉시 그린 뒤 프레임 단위로 전송한다. 서버는 수신 시각으로 `at`를 덮어쓰고 송신자를 포함한 모든 연결에 전달한다. 클라이언트는 `id`로 서버 응답을 합친다. 각 선분은 자신의 생성 시각부터 10초 동안 smoothstep 곡선으로 사라진다. 수명은 `server/main.go`의 `lifetime`에서 바꾼다.

서버는 매초 만료된 선을 제거한다. 상태와 클라이언트 캔버스는 최대 16,000개 선분으로 제한하며 초과 시 오래된 것부터 버린다. 메시지는 최대 32 KiB / 128개 선분이다. 느린 연결은 끊고 재접속 시 snapshot으로 복구한다. 연결이 끊겨도 로컬 그림은 그릴 수 있지만 공유하지 않으며, 재접속 시 서버 상태로 교체한다. 서버 재시작 시 그림은 사라진다. 공유 공간은 하나이며 접속 인원을 강제로 두 명으로 제한하지 않는다.

`server/websocket.go`는 텍스트 메시지·분할 프레임·ping/pong·close를 처리한다. 압축/확장은 사용하지 않는다. 웹소켓 프로토콜 변경 시 작은 Go 테스트와 브라우저 두 개의 실시간 공유를 확인한다.

수동 확인: 마우스/터치/펜으로 그리기, 탭으로 점 찍기, 두 화면 동시 입력, 10초 후 소멸, 새로고침 시 남은 그림 복원, 화면 크기 변경, 서버 재시작 후 재접속. UI·도구·로그인·장식은 추가하지 않는다.

## Caddy

설치 기본 경로는 `/etc/caddy/conf.d/bukupan.caddy`, `/srv/bukupan`이며 도메인은 `bukupan.localhost`다. 상위 Caddyfile의 import는 직접 추가한다. 설치를 다시 실행하면 해당 설정 파일과 세 정적 파일을 갱신한다. Caddy 실행 계정이 자산 경로의 상위 디렉터리까지 읽고 탐색할 수 있어야 한다. 자산은 복사하므로 Nix GC 후에도 남는다. 서버 프로세스 관리와 Caddy 리로드는 운영 환경에서 처리한다.
