# 개발

브라우저는 WebGL2 / Pointer Events / WebSocket과 순수 JavaScript, 서버는 Go 표준 라이브러리만 사용한다. 개발·테스트 도구는 `flake.lock`으로 고정하며 npm 설치 단계는 없다. 브라우저 테스트용 Playwright는 배포 패키지에 포함하지 않는다. WebGL2 미지원 환경에서는 안내만 표시하며 입력 처리와 서버 연결을 시작하지 않는다.

```sh
nix develop
./scripts/test fast
gofmt -w server/*.go
nixfmt flake.nix
```

`nix run .#dev-server`는 Go 서버(`127.0.0.1:3001`)와 Caddy(`127.0.0.1:3000`)를 함께 실행하고 종료 시 둘 다 정리한다. `nix run .#dev-server -- --host 192.168.0.6`으로 Caddy의 바인딩 인터페이스를 변경할 수 있다. 소스의 클라이언트를 바로 반영하려면 `BUKUPAN_CLIENT_ROOT="$PWD/client" nix run .#dev-server`로 실행한다. `BUKUPAN_DEV_ADDRESS`(기본 `:3000`)로 Caddy의 사이트 주소·포트를, `BUKUPAN_UPSTREAM`(기본 `127.0.0.1:3001`)으로 Go 서버의 주소를 변경할 수 있다. Go 변경 후에는 재시작한다.

flake는 `x86_64-linux`, `aarch64-linux`, `aarch64-darwin`만 지원한다. 평가 전에 새 파일을 `git add`해야 한다. Raspberry Pi에서는 해당 저장소에서 동일한 빌드·실행 명령을 쓴다.

## 테스트 실행 환경

기본 devShell은 Go·Python·Node·Caddy·C 컴파일러와 Linux의 systemd 검증 도구를 제공한다. `browser` devShell은 여기에 Playwright와 대응 브라우저를 추가한다. Nixpkgs의 테스트 러너 래퍼가 Node 모듈 경로와 브라우저 경로를 설정하므로 `npm install`이나 `playwright install`을 실행하지 않는다. 러너·브라우저 업데이트는 같은 `flake.lock` 변경으로 검증한다.

```sh
nix develop -c ./scripts/test fast          # Go와 JS 코덱
nix develop -c ./scripts/test race          # Go race detector
nix develop -c ./scripts/test integration   # CLI + 실제 Go 서버·Caddy
nix develop .#browser -c ./scripts/test browser
nix develop .#browser -c ./scripts/test all
nix flake check -L                         # 현재 플랫폼의 격리된 Nix checks
```

`scripts/test`가 공통 실행 절차이고 `nix/test-tools.nix`가 공통 도구 목록이다. devShell·checks·CI에 별도의 테스트 구현을 만들지 않는다. `checks`는 `unit`, `race`, `integration`, `browser`로 나누며 하나만 실행하려면 예를 들어 `nix build --no-link -L .#checks.x86_64-linux.browser`를 사용한다. Nix check가 캐시되어 있으면 재실행하지 않는다. 반복 재현에는 devShell 명령을 사용한다.

통합·브라우저 실행기는 현재 소스의 서버를 임시 경로에 빌드하고 `BUKUPAN_TEST_SERVER`를 자동으로 전달한다. 브라우저 테스트마다 새 서버·Caddy·임시 XDG 디렉터리·루프백 포트를 사용하고 종료 시 정리한다. 이미 실행 중인 개발 서버나 운영 데이터를 재사용하지 않는다. 필수 도구나 WebGL2가 없으면 실패해야 하며 이를 skip으로 통과시키지 않는다. Linux 전용 systemd 검사를 Darwin에서 제외하는 것은 명시적인 플랫폼 차이다.

브라우저 기본 검사는 Chromium + SwiftShader로 실제 WebGL2를 실행한다. GPU 종류에 따른 차이를 줄이는 정확성 검사이며 실제 GPU의 성능 검사는 아니다. `tests/browser`에는 기존 HTML 검사의 자동 실행과 실제 앱·포인터 입력·두 독립 브라우저 세션·새로고침 복원 검사가 있다. 상태 관찰용 렌더러 대역을 사용하는 HTML 검사와 실제 렌더러 검사를 구분한다.

```sh
# 하나의 검사 재현, 브라우저를 보며 실행 (GUI가 있는 개발 환경)
nix develop .#browser -c ./scripts/test browser -- --grep 'real pointer' --headed
# 간헐적 실패를 조사할 때만 반복
nix develop .#browser -c ./scripts/test browser -- --repeat-each=5
# 실패 보고서와 trace
nix develop .#browser -c playwright show-report test-results/report
nix develop .#browser -c playwright show-trace /path/to/trace.zip
```

실패 시 `test-results/`에 스크린샷·trace·브라우저 오류 및 서버 로그를 남긴다. 공유 검사에는 두 번째 참여자의 진단 자료도 포함한다. 결과물은 Git에 추가하지 않는다. Nix 빌드 내부의 실패는 `nix log`로 확인하고, 파일 자료가 필요하면 위 devShell 명령으로 재현한다.

`.github/workflows/test.yaml`은 PR과 main push에서 세 플랫폼의 unit·race·integration 및 패키지 빌드를 실행한다. 브라우저 기준 검사는 Linux에서 같은 devShell 실행기를 사용하여 실패 자료를 업로드한다. CI는 Cachix 쓰기 권한이나 배포 비밀값을 요구하지 않는다. 수동 캐시 업로드 워크플로는 별도로 유지한다.

JS 단위 검사는 `tests/unit/**/*.test.cjs`, 브라우저 검사는 `tests/browser/*.spec.cjs`로 추가하면 각 실행기가 발견한다. Go와 Python 검사는 기존 `*_test.go`, `test_*.py` 규칙을 따른다.

## 지속 가능한 테스트 정책

테스트는 사용자에게 약속한 동작과 데이터 보존 규칙을 보호한다. 변경의 위험과 검증 비용에 맞는 가장 작은 계층에 추가하고, 같은 경우를 모든 계층에 중복하지 않는다. 코드 줄 수나 커버리지 비율을 목표로 삼지 않는다.

| 변경 영역 | 기본 검증 위치 | 보호할 동작 |
| --- | --- | --- |
| 좌표·도형·문서 상태 계산 | Node 기본 테스트 러너 | 좌표 왕복, 경계값, 지우개와 실행 취소의 규칙 |
| 서버·바이너리 프로토콜 | Go `testing`, 공유 바이트 fixture | 입력 거부, 순서, 중복 방지, snapshot 일관성 |
| 저장·마이그레이션·운영 CLI | 실제 임시 저장소·서버 통합 검사 | 재시작 복원, 실패 처리, 이전 데이터 호환, 경로 보호 |
| 입력·브라우저·여러 사용자 연결 | Playwright | 실제 입력에서 공유 결과까지의 핵심 흐름 |
| 렌더링 | 실제 WebGL 픽셀 검사 | 위치·굵기·합성·삭제 결과와 GL 오류 |

- 버그 수정에는 가능하면 수정 전 실패하는 작은 재현 검사를 추가한다. 계산·상태 전이는 DOM·GPU·통신과 분리하여 직접 검증할 수 있게 한다. 문구·문서·단순한 스타일 변경에는 구현을 그대로 반복하는 테스트를 만들지 않는다.
- 새 기능의 정상 흐름과 중요한 실패·경계 조건을 함께 정의한다. 아직 구현하지 않은 기능을 위한 빈 테스트나 skip 목록은 만들지 않는다. 영속성을 도입할 때는 저장 확인 후 프로세스 종료·재시작, 응답 유실 후 재전송, 저장 실패, 마이그레이션과 백업 복원을 실제 임시 DB로 검사한다. 프로세스 강제 종료 검사를 전원 장애 보장의 증거로 취급하지 않는다.
- 두 사람의 편집 순서, 재접속, 지우기·실행 취소 등 공유 규칙은 서버 검사에서 자세히 다루고, 소수의 브라우저 검사로 실제 연결을 확인한다. 저장소의 정상 동작 자체를 mock으로 대신하지 않는다. 외부 운영 명령은 현재 CLI 테스트처럼 대역으로 격리한다.
- 임의의 긴 sleep보다 조건·이벤트를 기다리고 항상 제한 시간을 둔다. 시간 의존 로직은 가능한 한 제어 가능한 시계로 검증한다. 현재 클라이언트 통합 검사의 실제 10초 소멸 대기는 기존 계약을 검증하는 한 곳에만 남긴다. 무작위 검사를 추가하면 seed를 고정하거나 실패 시 출력한다.
- 기본 재시도는 0회다. 간헐적인 실패를 재시도 횟수 증가나 광범위한 예외 무시로 숨기지 않는다. 재현 자료를 남겨 격리·타이밍·상태 누출을 수정한다. 임시 skip이 불가피하면 좁은 조건과 사유, 추적 이슈 및 해제 조건을 함께 기록한다.
- 렌더링은 의미 있는 픽셀·기하·허용 오차를 검증한다. 전체 화면의 픽셀 일치를 무분별하게 고정하지 않는다. 기준 이미지가 필요하면 기대 결과의 변경을 직접 검토하고 갱신한다. 실패를 없애기 위한 일괄 갱신은 하지 않는다.
- 성능 검사는 고정 데이터셋과 명시한 환경으로 별도 측정한다. 공유 CI의 짧은 실행 시간 차이를 일반 기능 검사의 성공 조건으로 삼지 않는다. 대량 문서 기능을 도입하면 로딩 범위·캐시 상한 같은 결정적인 조건부터 검사한다.
- 같은 위험을 보호하지 못하는 중복 검사, 구현 세부사항에만 결합된 검사, 폐기된 기능의 검사는 변경과 함께 정리한다. 새 테스트 도구는 기존 Go·Node·Python·Playwright로 해결하기 어려운 구체적인 필요가 있을 때 도입한다.

수정 중에는 관련 suite를 실행하고, 공유·입력·렌더러 변경에는 브라우저 검사를 포함한다. 공통 실행기·Nix·테스트 환경 변경에는 전체 checks를 확인한다. 통과 후에는 새로운 변경이나 미해결 문제가 없는 한 같은 검사를 반복하지 않는다. PR에는 변경한 동작, 실행한 검사, 실행하지 못한 플랫폼·기기 검사를 적는다.

모바일 입력·펜·제스처·화면 회전·WebGL 관련 변경은 실제 사용하는 휴대폰에서도 확인한다. 브라우저 에뮬레이션을 실기기 확인으로 보고하지 않는다. 현재 소멸 기능은 아래 계약을 따르며, 영구 그림판으로 전환할 때 관련 코드·문서·검사를 같은 변경에서 갱신한다.

## 공유와 수명

`/ws`에 접속하면 서버 시각(`now`), 수명(`lifetime`, 밀리초), 아직 살아 있는 `segments`를 담은 `snapshot`을 받는다. 그다음 바이너리 draw를 주고받는다. 선분은 16바이트 클라이언트 ID, uint32 순번, 좌표 네 개와 서버 생성 시각으로 구성되며 좌표는 가상 정사각형 공간의 0–1 비율이다. 각 화면의 긴 변을 M으로 삼아 M × M 공간을 화면 중앙에 배치한다. 입력과 렌더링 모두 같은 변환을 사용하며 화면 밖 가장자리는 잘린다. 화면 비율이 달라도 그림의 가로세로 비율은 유지된다. 선 두께는 3 CSS px다.

WebGL2는 둥근 선분을 인스턴싱으로 한 번에 그리며, 픽셀 밝기의 최댓값(`MAX`)으로 합성해 접합부가 밝게 누적되지 않는다. 좌표·생성 시각 버퍼는 선분 변경 시에만 갱신하고 소멸 계산은 셰이더에서 처리한다. 컨텍스트 복구 시 버퍼를 재생성한다.

클라이언트는 즉시 그린 뒤 프레임 단위로 전송한다. 서버는 수신 시각으로 `at`를 덮어쓰고 송신자를 포함한 모든 연결에 전달한다. 클라이언트는 클라이언트 ID와 순번으로 서버 응답을 합친다. 로컬 좌표도 float32로 맞춘다. 순번 소진 전 새 무작위 ID를 생성한다. 각 선분은 자신의 생성 시각부터 10초 동안 smoothstep 곡선으로 사라진다. 수명은 `server/main.go`의 `lifetime`에서 바꾼다.

서버는 미리 할당한 16,000칸 링 버퍼에 선분을 보관하고 매초 앞에서부터 만료된 선을 제거한다. 기록 시각은 방 잠금 안에서 직전 기록 시각 이상으로 보정한다. 상태와 클라이언트 캔버스는 최대 16,000개 선분으로 제한하며 초과 시 오래된 것부터 버린다. 입력 메시지는 최대 32 KiB / 128개 선분이다. 출력 snapshot은 최대 704,016바이트를 허용한다. 느린 연결은 끊고 재접속 시 snapshot으로 복구한다. 연결이 끊겨도 로컬 그림은 그릴 수 있지만 공유하지 않으며, 재접속 시 서버 상태로 교체한다. 서버 재시작 시 그림은 사라진다. 공유 공간은 하나이며 접속 인원을 강제로 두 명으로 제한하지 않는다.

`server/websocket.go`는 바이너리 메시지·분할 프레임·ping/pong·close를 처리한다. 텍스트 데이터 메시지는 거부한다. 바이너리에는 UTF-8 검사를 적용하지 않고 close 사유에는 적용한다. 압축/확장은 사용하지 않는다. 웹소켓 프로토콜 변경 시 작은 Go 테스트와 브라우저 두 개의 실시간 공유를 확인한다.

`scripts/test-renderer.html`을 브라우저로 열면 실제 WebGL2 픽셀을 읽어 접합부와 본체 밝기 일치, 점, 16,000개 인스턴스, 소멸과 GL 오류를 검사한다. 성공 시 PASS를 표시한다.

수동 확인: 마우스/터치/펜으로 그리기, 탭으로 점 찍기, 두 화면 동시 입력, 10초 후 소멸, 새로고침 시 남은 그림 복원, 화면 크기 변경, 서버 재시작 후 재접속.

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
node --test tests/unit/codec.test.cjs
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
nix develop -c ./scripts/test integration
nix flake check
nix build .#bukupan .#bukupan-server .#bukupan-client .#dev-server
```

CLI 테스트는 임시 XDG 디렉터리와 systemctl·loginctl·nix-store 대역을 사용한다. 실제 사용자 서비스는 조작하지 않는다. `integration` suite는 실제 Go 백엔드와 임시 Caddy로 정적 파일·healthz·WebSocket 연결도 확인한다. Python unittest를 직접 실행하면 `BUKUPAN_TEST_SERVER`가 필요한 검사가 생략될 수 있으므로 전체 검증에는 공통 실행기를 사용한다. Linux에서는 공백과 `%`를 포함한 유닛 경로를 `systemd-analyze verify`로 확인한다. Python 캐시는 Git에서 제외한다.

운영·전환 절차는 README를 따른다. Caddy master 설정 import와 검증·적용은 사용자가 수행한다. 기존 설치 파일과 복사 자산을 자동 인수·삭제하지 않는다.
