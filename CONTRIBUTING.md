# 개발

브라우저는 WebGL2 / Pointer Events / WebSocket과 순수 JavaScript, 서버는 Go 표준 라이브러리만 사용한다. npm이나 별도 모듈 설치는 없다. WebGL2 미지원 환경에서는 안내만 표시하며 입력 처리와 서버 연결을 시작하지 않는다.

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

`/ws`에 접속하면 서버 시각(`now`), 수명(`lifetime`, 밀리초), 아직 살아 있는 `segments`를 담은 `snapshot`을 받는다. 그다음 바이너리 draw를 주고받는다. 선분은 16바이트 클라이언트 ID, uint32 순번, 좌표 네 개와 서버 생성 시각으로 구성되며 좌표는 가상 정사각형 공간의 0–1 비율이다. 각 화면의 긴 변을 M으로 삼아 M × M 공간을 화면 중앙에 배치한다. 입력과 렌더링 모두 같은 변환을 사용하며 화면 밖 가장자리는 잘린다. 화면 비율이 달라도 그림의 가로세로 비율은 유지된다. 선 두께는 3 CSS px다.

WebGL2는 둥근 선분을 인스턴싱으로 한 번에 그리며, 픽셀 밝기의 최댓값(`MAX`)으로 합성해 접합부가 밝게 누적되지 않는다. 좌표·생성 시각 버퍼는 선분 변경 시에만 갱신하고 소멸 계산은 셰이더에서 처리한다. 컨텍스트 복구 시 버퍼를 재생성한다.

클라이언트는 즉시 그린 뒤 프레임 단위로 전송한다. 서버는 수신 시각으로 `at`를 덮어쓰고 송신자를 포함한 모든 연결에 전달한다. 클라이언트는 클라이언트 ID와 순번으로 서버 응답을 합친다. 로컬 좌표도 float32로 맞춘다. 순번 소진 전 새 무작위 ID를 생성한다. 각 선분은 자신의 생성 시각부터 10초 동안 smoothstep 곡선으로 사라진다. 수명은 `server/main.go`의 `lifetime`에서 바꾼다.

서버는 미리 할당한 16,000칸 링 버퍼에 선분을 보관하고 매초 앞에서부터 만료된 선을 제거한다. 기록 시각은 방 잠금 안에서 직전 기록 시각 이상으로 보정한다. 상태와 클라이언트 캔버스는 최대 16,000개 선분으로 제한하며 초과 시 오래된 것부터 버린다. 입력 메시지는 최대 32 KiB / 128개 선분이다. 출력 snapshot은 최대 704,016바이트를 허용한다. 느린 연결은 끊고 재접속 시 snapshot으로 복구한다. 연결이 끊겨도 로컬 그림은 그릴 수 있지만 공유하지 않으며, 재접속 시 서버 상태로 교체한다. 서버 재시작 시 그림은 사라진다. 공유 공간은 하나이며 접속 인원을 강제로 두 명으로 제한하지 않는다.

`server/websocket.go`는 바이너리 메시지·분할 프레임·ping/pong·close를 처리한다. 텍스트 데이터 메시지는 거부한다. 바이너리에는 UTF-8 검사를 적용하지 않고 close 사유에는 적용한다. 압축/확장은 사용하지 않는다. 웹소켓 프로토콜 변경 시 작은 Go 테스트와 브라우저 두 개의 실시간 공유를 확인한다.

`scripts/test-renderer.html`을 브라우저로 열면 실제 WebGL2 픽셀을 읽어 접합부와 본체 밝기 일치, 점, 16,000개 인스턴스, 소멸과 GL 오류를 검사한다. 성공 시 PASS를 표시한다.

수동 확인: 마우스/터치/펜으로 그리기, 탭으로 점 찍기, 두 화면 동시 입력, 10초 후 소멸, 새로고침 시 남은 그림 복원, 화면 크기 변경, 서버 재시작 후 재접속. UI·도구·로그인·장식은 추가하지 않는다.

## 바이너리 프로토콜 v1

모든 다중 바이트 값은 little-endian이다. `client/codec.js`를 앱보다 먼저 로드하며 WebSocket `binaryType`은 `arraybuffer`다. JSON 호환 계층은 없다. 서버와 클라이언트를 함께 전환하고 기존 탭은 새로고침해야 한다. 실제 서비스 배포는 별도로 수행한다.

공통 헤더(16바이트):

| 오프셋 | 형식 | 값 |
| --- | --- | --- |
| 0 | uint8 | 버전 1 |
| 1 | uint8 | 입력 draw 1, 서버 draw 2, snapshot 3 |
| 2 | uint16 | 선분 개수 |
| 4 | uint64 | 서버 시각(ms) |
| 12 | uint32 | 수명(ms) |

선분 레코드(36바이트, snapshot은 44바이트):

| 레코드 오프셋 | 형식 | 값 |
| --- | --- | --- |
| 0 | 16 raw bytes | 무작위 클라이언트 ID |
| 16 | uint32 | 순번 |
| 20, 24, 28, 32 | float32 | x1, y1, x2, y2 |
| 36 (snapshot만) | uint64 | 생성 시각(ms) |

입력 draw는 시각·수명이 0이고 1–128개를 담는다. 서버 draw는 공통 수신 시각과 수명 0을 쓰며 모든 선분에 같은 시각을 적용한다. snapshot은 현재 시각, 수명 10,000ms와 0–16,000개를 담는다. 길이는 정확히 `16 + 개수 × 레코드 크기`여야 한다. 좌표는 유한한 0–1 값이어야 한다. 서버는 버전·종류·개수·길이·예약 필드(입력 시각/수명)의 0 값을 검증한다. 브라우저는 서버 메시지의 종류·길이·개수·좌표와 안전한 정수 범위의 시각을 검증하고, 잘못된 메시지는 연결을 닫아 재접속 경로로 처리한다.

`testdata/*.hex`는 Go와 JavaScript가 공유하는 고정 바이트 예제다.

```sh
node scripts/test-codec.js
```

`scripts/test-client.html`은 `/ws`를 실제 Go 서버에 연결하고 저장소 루트를 정적으로 제공한 환경에서 실행한다. 두 iframe의 실제 앱과 WebSocket을 사용해 즉시 로컬 표시·공유·응답 병합·재접속 snapshot·10초 소멸·잘못된 서버 메시지 후 재접속을 검사한다. 렌더러는 관찰용 대역이며 실제 WebGL 검사는 `scripts/test-renderer.html`로 별도 수행한다. 성능 측정과 벤치마크는 포함하지 않는다.

## 웹 아이콘과 공유 카드

웹 탭은 `client/icon.svg`, 설치 아이콘은 타원을 제거하고 전체를 분홍색으로 채운 PNG를 사용한다. `scripts/generate-icons.py`가 SVG의 색상과 선을 읽어 Apple touch 및 PWA 아이콘을 생성한다.

```sh
nix shell --inputs-from . nixpkgs#python3 nixpkgs#resvg --command python3 scripts/generate-icons.py
```

공유 카드는 `scripts/generate-og-image.py`로 생성한다. Noto Sans CJK KR 폰트 파일 경로를 인자로 전달한다.

```sh
nix build --inputs-from . nixpkgs#noto-fonts-cjk-sans --no-link --print-out-paths
# 위 출력 경로의 share/fonts/opentype/noto-cjk/NotoSansCJK-VF.otf.ttc 사용
nix shell --inputs-from . nixpkgs#python3 nixpkgs#resvg --command python3 scripts/generate-og-image.py /path/to/NotoSansCJK-VF.otf.ttc
```

공유 메타데이터의 배포 주소는 `client/index.html`의 `https://bukupan.hooreique.com/`이다. 도메인을 바꿀 때 canonical·Open Graph·Twitter 이미지 주소도 함께 수정한다. SNS는 HTML의 정적 메타데이터와 `client/og-image.png`를 사용하며 JavaScript 실행에 의존하지 않는다.

## 운영 CLI와 검증

`cli/bukupan.py`는 Python 표준 라이브러리만 사용한다. `cli/package.nix`가 서버·클라이언트·Caddy 템플릿 store 경로와 실행 의존성을 주입한다. `site`는 사이트 파일과 사이트별 GC root만, `service`는 Linux systemd 사용자 서비스와 백엔드 GC root만 관리한다. `serve`는 `exec`로 서버를 실행한다. 서버 Go 플래그와 공유 프로토콜은 독립적으로 유지한다.

```sh
python3 -m unittest discover -s cli -v
nix flake check
nix build .#bukupan .#bukupan-server .#bukupan-client .#dev-server
```

CLI 테스트는 임시 XDG 디렉터리와 systemctl·loginctl·nix-store 대역을 사용한다. 실제 사용자 서비스는 조작하지 않는다. `nix flake check`는 실제 Go 백엔드와 임시 Caddy로 정적 파일·healthz·WebSocket 연결도 확인한다. 로컬에서 같은 검증을 하려면 Go 서버를 빌드하고 `BUKUPAN_TEST_SERVER`에 실행 파일 경로를 지정한다. Linux에서는 공백과 `%`를 포함한 유닛 경로를 `systemd-analyze verify`로 확인한다. Python 캐시는 Git에서 제외한다.

운영·전환 절차는 README를 따른다. Caddy master 설정 import와 검증·적용은 사용자가 수행한다. 기존 설치 파일과 복사 자산을 자동 인수·삭제하지 않는다.
