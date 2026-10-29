# 개발

브라우저는 WebGL2 / Pointer Events / WebSocket과 순수 JavaScript, 서버는 Go와 `go-sqlite3`의 내장 SQLite를 사용한다. SQLite 드라이버는 `go.mod`·`go.sum`과 Nix `vendorHash`로 고정하며 CGO 빌드가 필요하다. 개발·테스트 도구는 `flake.lock`으로 고정하며 npm 설치 단계는 없다. 브라우저 테스트용 Playwright는 배포 패키지에 포함하지 않는다. WebGL2 미지원 환경에서는 안내만 표시하며 입력 처리와 서버 연결을 시작하지 않는다.

```sh
nix develop
./scripts/test fast
gofmt -w server/*.go
nixfmt flake.nix
```

`nix run .#dev-server`는 Go 서버(`127.0.0.1:3001`)와 Caddy(`127.0.0.1:3000`)를 함께 실행하고 종료 시 둘 다 정리한다. `nix run .#dev-server -- --host 192.168.0.6`으로 Caddy의 바인딩 인터페이스를 변경할 수 있다. 소스의 클라이언트를 바로 반영하려면 `BUKUPAN_CLIENT_ROOT="$PWD/client" nix run .#dev-server`로 실행한다. `BUKUPAN_DEV_ADDRESS`(기본 `:3000`)로 Caddy의 사이트 주소·포트를, `BUKUPAN_UPSTREAM`(기본 `127.0.0.1:3001`)으로 Go 서버의 주소를 변경할 수 있다. Go 변경 후에는 재시작한다. 개발 문서는 `$XDG_DATA_HOME/bukupan-dev/board.sqlite`에 따로 보존하며 `BUKUPAN_DEV_DB`로 바꾼다. 운영 문서를 개발용으로 재사용하지 않는다.

flake는 `x86_64-linux`, `aarch64-linux`, `aarch64-darwin`만 지원한다. 평가 전에 새 파일을 `git add`해야 한다. Raspberry Pi에서는 해당 저장소에서 동일한 빌드·실행 명령을 쓴다.

## 테스트 실행 환경

기본 devShell은 Go·Python·Node·Caddy·C 컴파일러와 Linux의 systemd 검증 도구를 제공한다. `browser` devShell은 여기에 Playwright와 대응 브라우저를 추가한다. Nixpkgs의 테스트 러너 래퍼가 Node 모듈 경로와 브라우저 경로를 설정하므로 `npm install`이나 `playwright install`을 실행하지 않는다. 러너·브라우저 업데이트는 같은 `flake.lock` 변경으로 검증한다.

```sh
nix develop -c ./scripts/test fast          # Go 저장·공유 규칙 + JS 좌표·프로토콜
nix develop -c ./scripts/test race          # Go race detector
nix develop -c ./scripts/test integration   # CLI + 실제 Go 서버·Caddy
nix develop .#browser -c ./scripts/test browser
nix develop .#browser -c ./scripts/test all
nix flake check -L                         # 현재 플랫폼의 격리된 Nix checks
```

`scripts/test`가 공통 실행 절차이고 `nix/test-tools.nix`가 공통 도구 목록이다. devShell·checks·CI에 별도의 테스트 구현을 만들지 않는다. `checks`는 `unit`, `race`, `integration`, `browser`로 나누며 하나만 실행하려면 예를 들어 `nix build --no-link -L .#checks.x86_64-linux.browser`를 사용한다. Nix check가 캐시되어 있으면 재실행하지 않는다. 반복 재현에는 devShell 명령을 사용한다.

통합·브라우저 실행기는 현재 소스의 서버를 임시 경로에 빌드하고 `BUKUPAN_TEST_SERVER`를 자동으로 전달한다. 브라우저 테스트마다 새 서버·Caddy·임시 XDG 디렉터리·루프백 포트를 사용하고 종료 시 정리한다. 이미 실행 중인 개발 서버나 운영 데이터를 재사용하지 않는다. 필수 도구나 WebGL2가 없으면 실패해야 하며 이를 skip으로 통과시키지 않는다. Linux 전용 systemd 검사를 Darwin에서 제외하는 것은 명시적인 플랫폼 차이다.

브라우저 기본 검사는 Chromium + SwiftShader로 실제 WebGL2를 실행한다. GPU 종류에 따른 차이를 줄이는 정확성 검사이며 실제 GPU의 성능 검사는 아니다. `tests/browser`는 실제 앱·포인터 입력·두 독립 세션·부분 지우기·내 작업 취소·카메라·터치·응답 유실 후 재전송·프로세스 강제 종료 후 복원을 확인한다. 렌더러 픽셀 검사는 `scripts/test-renderer.html`을 실행한다.

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
| 서버·바이너리 프로토콜 | Go `testing`, 공유 프로토콜 fixture | 입력 거부, 순서, 중복 방지, snapshot 일관성 |
| 저장·마이그레이션·운영 CLI | 실제 임시 저장소·서버 통합 검사 | 재시작 복원, 실패 처리, 이전 데이터 호환, 경로 보호 |
| 입력·브라우저·여러 사용자 연결 | Playwright | 실제 입력에서 공유 결과까지의 핵심 흐름 |
| 렌더링 | 실제 WebGL 픽셀 검사 | 위치·굵기·합성·삭제 결과와 GL 오류 |

- 버그 수정에는 가능하면 수정 전 실패하는 작은 재현 검사를 추가한다. 계산·상태 전이는 DOM·GPU·통신과 분리하여 직접 검증할 수 있게 한다. 문구·문서·단순한 스타일 변경에는 구현을 그대로 반복하는 테스트를 만들지 않는다.
- 새 기능의 정상 흐름과 중요한 실패·경계 조건을 함께 정의한다. 아직 구현하지 않은 기능을 위한 빈 테스트나 skip 목록은 만들지 않는다. 영속성을 도입할 때는 저장 확인 후 프로세스 종료·재시작, 응답 유실 후 재전송, 저장 실패, 마이그레이션과 백업 복원을 실제 임시 DB로 검사한다. 프로세스 강제 종료 검사를 전원 장애 보장의 증거로 취급하지 않는다.
- 두 사람의 편집 순서, 재접속, 지우기·실행 취소 등 공유 규칙은 서버 검사에서 자세히 다루고, 소수의 브라우저 검사로 실제 연결을 확인한다. 저장소의 정상 동작 자체를 mock으로 대신하지 않는다. 외부 운영 명령은 현재 CLI 테스트처럼 대역으로 격리한다.
- 임의의 긴 sleep보다 조건·이벤트를 기다리고 항상 제한 시간을 둔다. 시간 의존 로직은 가능한 한 제어 가능한 시계로 검증한다. 무작위 검사를 추가하면 seed를 고정하거나 실패 시 출력한다.
- 기본 재시도는 0회다. 간헐적인 실패를 재시도 횟수 증가나 광범위한 예외 무시로 숨기지 않는다. 재현 자료를 남겨 격리·타이밍·상태 누출을 수정한다. 임시 skip이 불가피하면 좁은 조건과 사유, 추적 이슈 및 해제 조건을 함께 기록한다.
- 렌더링은 의미 있는 픽셀·기하·허용 오차를 검증한다. 전체 화면의 픽셀 일치를 무분별하게 고정하지 않는다. 기준 이미지가 필요하면 기대 결과의 변경을 직접 검토하고 갱신한다. 실패를 없애기 위한 일괄 갱신은 하지 않는다.
- 성능 검사는 고정 데이터셋과 명시한 환경으로 별도 측정한다. 공유 CI의 짧은 실행 시간 차이를 일반 기능 검사의 성공 조건으로 삼지 않는다. 대량 문서 기능을 도입하면 로딩 범위·캐시 상한 같은 결정적인 조건부터 검사한다.
- 같은 위험을 보호하지 못하는 중복 검사, 구현 세부사항에만 결합된 검사, 폐기된 기능의 검사는 변경과 함께 정리한다. 새 테스트 도구는 기존 Go·Node·Python·Playwright로 해결하기 어려운 구체적인 필요가 있을 때 도입한다.

수정 중에는 관련 suite를 실행하고, 공유·입력·렌더러 변경에는 브라우저 검사를 포함한다. 공통 실행기·Nix·테스트 환경 변경에는 전체 checks를 확인한다. 통과 후에는 새로운 변경이나 미해결 문제가 없는 한 같은 검사를 반복하지 않는다. PR에는 변경한 동작, 실행한 검사, 실행하지 못한 플랫폼·기기 검사를 적는다.

모바일 입력·펜·제스처·화면 회전·WebGL 관련 변경은 실제 사용하는 휴대폰에서도 확인한다. 브라우저 에뮬레이션을 실기기 확인으로 보고하지 않는다. 실기기 확인이 불가능하면 그 한계를 검증 결과에 명시한다.

## 영구 문서와 카메라

서버 하나가 SQLite 문서 하나를 소유한다. 기본 경로는 `$XDG_DATA_HOME/bukupan/board.sqlite`(`~/.local/share/bukupan/board.sqlite`)이고 `-db`로 바꾼다. 실행 프로세스는 DB 옆 `.lock` 파일의 운영체제 잠금을 잡아 중복 서빙을 막는다. 서버 종료·재접속·줌아웃은 그림을 삭제하지 않는다. 구 버전의 메모리상 10초 그림은 이 문서로 이전하지 않는다.

문서는 float64 세계 좌표를 사용한다. 원점은 처음 화면의 가운데이고, 배율 100%에서 1단위 = 1 CSS px다. 펜 굵기는 세계 좌표 3단위로 고정한다. 지우개는 화면에서 지름 28 CSS px인 원이 이동한 영역을 지운다. 좌표는 축마다 ±10억 단위 범위이며 화면은 25–800%로 확대할 수 있다. 매우 큰 뷰포트는 요청 영역 상한에 맞춰 최소 배율을 높인다. 이는 유한한 정밀도와 자원 한도를 둔 가상 무한 공간이다.

각 브라우저가 카메라를 따로 보관한다. `client/world.js`는 좌표 변환·커서 기준 확대·경계를 담당한다. WebGL2에 넘기기 전에 double 좌표에서 카메라 위치를 빼므로 멀리 이동해도 float32 정밀도를 잃지 않는다. 렌더러는 둥근 선분을 인스턴싱하고 `MAX`로 합성한다. 그림·카메라·크기 변경 때만 렌더링하며 컨텍스트 복구 시 버퍼를 재생성한다.

한 번의 드래그는 하나의 action이다. 네트워크 전송은 짧은 청크로 나누되 실행 취소는 action 전체에 적용한다. 지우개는 기존 선분과 이동 원의 교차 구간을 기록한다. 자기 펜 취소는 해당 action의 선만 숨기고, 자기 지우개 취소는 해당 마스크만 제거한다. 다른 사람의 후속 그림과 다른 지우개 마스크는 보존한다. 실행 취소는 자기 최근 작업부터 제공하며 redo는 없다. 원래 선·마스크·작업·영수증은 취소와 중복 방지를 위해 DB에 보존하고 자동 정리하지 않는다. 저장 용량은 문서 이력과 함께 증가한다.

`server/store.go`는 WAL + `synchronous=FULL` 트랜잭션 안에서 작업·공간 인덱스·마스크·문서 revision·영수증을 함께 기록한다. revision은 성공한 작업마다 증가한다. 같은 작업 ID와 내용의 재전송은 기존 영수증을 반환하고, 같은 ID에 다른 내용을 실으면 거부한다. 저장 실패는 롤백하며 ACK를 보내지 않는다. `user_version`으로 스키마를 구분하고 미래 버전은 실행을 거부한다. 스키마 변경에는 실제 이전 버전 파일로 마이그레이션 검사를 추가한다.

1024단위 셀 인덱스로 현재 화면 주변만 조회한다. 요청 영역의 한 변은 최대 32,768단위, 한 번의 입력은 최대 128점·8,192단위 영역이다. 화면에는 최대 4,000개 stroke 청크·40,000개 표시 선분을 허용한다. 초과하면 확대 안내를 표시하며 오래된 그림을 버리지 않는다. 완전히 지워진 선은 조회 대상에서 제외하고 실행 취소 시 되살린다. 클라이언트는 화면 이동 시 캐시를 교체하고, patch 누적에도 같은 상한을 검사한다. 느린 연결은 최대 8 MiB 출력 큐를 넘으면 끊고 snapshot으로 복원한다.

## 동기화와 저장 상태

`client/outbox.js`는 IndexedDB에 미전송 작업을 기록한다. 앱은 로컬 기록이 완료된 뒤 서버에 보내고, 서버는 SQLite 커밋 뒤 ACK를 보낸다. ACK를 받은 후 로컬 기록을 지운다. 그래서 응답 유실·탭 새로고침·재접속에서는 같은 ID로 재전송해 중복을 막는다. 기기 저장 실패나 서버 저장 실패는 화면에 표시하고 작업을 보존한다. 미전송 작업은 JSON으로 내보낼 수 있으며, 명시적인 확인 후 제외할 수 있다. 내보내기는 장애 복구 자료이고 자동 가져오기 기능은 아직 없다.

연결 중에만 새 편집을 시작한다. 연결이 끊기는 순간의 진행 중 선은 로컬 큐에 넣고 재접속 때 전송한다. 펜 미리보기는 즉시 표시하고 지우개 결과는 서버 응답으로 반영한다. 로컬 대기 큐는 128개 청크까지이며 초과하면 새 입력을 멈춘다. `저장됨`은 미전송 작업이 없고 서버의 화면을 받은 상태다. 브라우저 데이터 삭제는 미전송 작업·자기 작업 ID·카메라를 잃게 하지만 이미 서버에 저장된 그림은 유지된다. 다른 board ID의 대기 작업은 새 문서에 자동 적용하지 않는다.

사용자 ID는 브라우저의 무작위 ID이며 인증 수단이 아니다. 접근 가능한 사람은 같은 문서를 그리고 지울 수 있다. 인증·접근 제한이 필요한 배포는 Caddy 앞단에서 설정한다. 여러 서버 프로세스가 하나의 DB를 동시에 서빙하거나 네트워크 파일 시스템에 DB를 두는 구성은 지원하지 않는다.

## 프로토콜 v2

`/ws`는 바이너리 프레임에 UTF-8 JSON을 담는다. 변경이 잦은 문서·영수증·에러 스키마를 명시적으로 검사하고, 세계 좌표의 double 정밀도를 유지한다. 이전의 고정 float32 레코드 v1과 호환되지 않으므로 서버·정적 클라이언트를 함께 배포하고 기존 탭을 새로고침한다. 별도 압축·텍스트 프레임은 사용하지 않는다.

모든 메시지에 `v: 2`, `type`이 있다. 서버 메시지에는 `revision`이 있고 ID는 소문자 32자리 hex다. 입력은 32 KiB, 출력은 8 MiB 이하다. 입력의 미지 필드·잘못된 버전·크기·좌표는 거부한다. 브라우저도 타입·ID·배열 크기·좌표·안전한 정수 범위를 검증한다. WebSocket 프레이밍 검사는 `server/websocket_test.go`, 공통 예제는 `testdata/protocol-v2.json`으로 유지한다.

| 방향·type | 주요 필드와 의미 |
| --- | --- |
| 서버 `hello` | `board`: 영구 문서 ID |
| 입력 `view` | `board`, `author`, `view`: 요청 번호, `area`: `[x0,y0,x1,y1]` |
| 서버 `snapshot` | `view`, `revision`, `drawings`, `history`: 해당 화면 전체와 자기 최근 action ID 최대 50개 |
| 입력 `edit` | `board`, `edit`: 작업 객체 |
| 서버 `patch` | `view`, `revision`, `drawings`: 변경된 stroke 교체; 빈 `lines`는 표시에서 제거 |
| 서버 `ack` | `id`: 저장된 작업 ID, `revision`, `history` |
| 서버 `error` | `code`: storage/dense/invalid/board, 관련 `id` 또는 `view` |

펜·지우개 작업은 `{id, author, action, kind: "pen" 또는 "erase", width, points: [[x,y],…]}`다. 취소는 `{id, author, kind: "undo", target: actionID}`다. 표시용 drawing은 `{id, width, lines: [[x1,y1,x2,y2],…]}`다. 서버가 순서대로 적용하고 송신자에게도 patch를 보낸 뒤 ACK한다. 오래된 view 번호의 응답은 버리고, 연결이 바뀌면 새 snapshot을 받는다. 다른 참여자의 카메라는 전송하지 않는다.

수동 확인: 실제 휴대폰에서 한 손가락 그리기·두 손가락 이동/확대·펜 입력·화면 회전, 두 화면 동시 편집, 확대 후 지우기와 취소, 연결 단절 안내, 먼 위치에서 선의 정밀도, WebGL 컨텍스트 복구. SwiftShader의 자동 검사만으로 실제 GPU 성능이나 모바일 조작감을 보장하지 않는다.

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

`cli/bukupan.py`는 Python 표준 라이브러리만 사용한다. `cli/package.nix`가 서버·클라이언트·Caddy 템플릿 store 경로와 실행 의존성을 주입한다. `site`는 사이트 파일과 사이트별 GC root만, `service`는 Linux systemd 사용자 서비스와 백엔드 GC root만 관리한다. `serve`는 `exec`로 서버를 실행한다. `serve`·`service install`의 `--database`는 절대 경로로 서버에 전달한다. 서비스 제거는 DB에 손대지 않는다. `backup --database … --output …`은 서버의 일관된 백업 기능을 실행한다. 서버 Go 플래그와 공유 프로토콜은 독립적으로 유지한다.

```sh
nix develop -c ./scripts/test integration
nix flake check
nix build .#bukupan .#bukupan-server .#bukupan-client .#dev-server
```

CLI 테스트는 임시 XDG 디렉터리와 systemctl·loginctl·nix-store 대역을 사용한다. 실제 사용자 서비스는 조작하지 않는다. `integration` suite는 실제 Go 백엔드와 임시 Caddy로 정적 파일·healthz·WebSocket 연결도 확인한다. Python unittest를 직접 실행하면 `BUKUPAN_TEST_SERVER`가 필요한 검사가 생략될 수 있으므로 전체 검증에는 공통 실행기를 사용한다. Linux에서는 공백과 `%`를 포함한 유닛 경로를 `systemd-analyze verify`로 확인한다. Python 캐시는 Git에서 제외한다. 실제 서버를 실행해 DB 중복 서빙 거부와 실행 중 백업·기존 백업 보호도 확인한다. 세 플랫폼의 Nix checks에는 같은 SQLite 소스를 vendor로 공급하므로 sandbox에서 Go 모듈을 다운로드하지 않는다.

운영·전환 절차는 README를 따른다. Caddy master 설정 import와 검증·적용은 사용자가 수행한다. 기존 설치 파일과 복사 자산을 자동 인수·삭제하지 않는다.
