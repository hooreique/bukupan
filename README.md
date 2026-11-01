# 부끄판

검은 화면에 함께 그리는 흰 선. 그림은 SQLite에 남고, 각자 넓은 공간을 움직이며 함께 그린다. 펜·부분 지우개·내 작업 취소, 이동·확대·처음 위치 복귀를 지원한다. 두 손가락은 이동과 확대, 마우스 휠은 확대, Space + 드래그는 이동이다.

기본 저장 위치는 `$XDG_DATA_HOME/bukupan/board.sqlite`(기본 `~/.local/share/bukupan/board.sqlite`)다. 인증은 없으므로 접속 가능한 사람 모두가 같은 문서를 편집한다. 문서는 자동 만료되지 않으며 서버 재시작·새로고침 후에도 남는다.

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
bukupan service uninstall  # 사이트·그림 데이터 보존; --disable-linger로만 linger 해제
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

## 이 서버에서 배포하기 (hooddy)

이 저장소에서 “배포해줘”라는 지시를 받으면 아래 절차로 현재 소스를 빌드하고 운영 서비스를 갱신한다. 문서 수정만 요청한 경우에는 배포하지 않는다. `~/ws/buku-journal`과 동일하게 Nix 패키지, systemd 사용자 서비스, hooddy의 일반 Caddy 사이트를 사용한다. 이 저장소에는 `nix run .#deploy` 명령이 없으며 아래 단계를 직접 수행한다.

현재 `song` 사용자의 운영 구성은 다음과 같다. 다른 계정이나 서버에서는 먼저 실제 설정을 확인한다.

| 항목 | 값 |
| --- | --- |
| 작업 디렉터리 | `/home/song/ws/bukupan` |
| 외부 주소 | `https://bukupan.hooreique.com` |
| 백엔드 | `127.0.0.1:3001` (CLI 기본값 8080과 다름) |
| 운영 DB | `/home/song/.local/share/bukupan/board.sqlite` |
| 서비스 | `bukupan.service` (systemd 사용자 서비스) |
| 서비스 설정 | `~/.config/bukupan/service.env` |
| 사이트 파일 | `~/.config/hooddy/sites.d/bukupan.caddy` |
| Caddy master | `~/.config/hooddy/Caddyfile` (`import sites.d/*.caddy` 포함) |
| Caddy 실행 파일 | `~/.config/hooddy/caddy/bin/caddy` |

명령은 이 서버의 `song` 사용자로 저장소 루트에서 실행한다. hooddy가 설치·실행 중이고 linger가 활성화되어 있어야 한다. 도메인과 외부 HTTPS 연결은 기존 hooddy 구성을 사용한다. master 설정과 다른 사이트는 그대로 유지한다.

1. **운영 설정과 변경 사항 확인, 검증 및 빌드.** `service.env`의 DB 경로와 주소가 위 표와 다르면 실제 값을 이후 명령에도 반영한다. 새 파일이 Nix 소스에 포함되었는지 확인하고, 배포할 서버와 클라이언트를 같은 소스에서 빌드한다. 이 단계는 서비스 정지 전에 완료한다.

   ```sh
   git status --short
   cat ~/.config/bukupan/service.env
   systemctl --user cat bukupan.service
   systemctl --user status hooddy.service --no-pager
   loginctl show-user "$(id -un)" --property=Linger
   nix flake check
   nix build .#bukupan
   ```

2. **운영 DB 백업.** 실행 중에도 일관된 사본을 만들 수 있다. 출력 경로는 매번 새로 만들고, 백업이 성공한 뒤 진행한다.

   ```sh
   backup_dir=$(mktemp -d "$HOME/.local/state/bukupan/deploy-backup.XXXXXX")
   ./result/bin/bukupan backup \
     --database /home/song/.local/share/bukupan/board.sqlite \
     --output "$backup_dir/board.sqlite"
   cp ~/.config/bukupan/service.env "$backup_dir/service.env"
   cp ~/.config/systemd/user/bukupan.service "$backup_dir/bukupan.service"
   cp ~/.config/hooddy/sites.d/bukupan.caddy "$backup_dir/bukupan.caddy"
   ```

3. **서비스와 사이트 갱신.** 서비스 설치는 기존 설치를 덮어쓰지 않으므로 제거 후 재설치한다. 이 사이에는 잠시 접속이 중단된다. 제거해도 운영 DB와 사이트 파일은 보존된다. `restart`만으로는 새 패키지가 적용되지 않는다. 최초 설치라면 `uninstall`을 생략하고, linger가 꺼져 있으면 설치에 `--enable-linger`를 추가한다. 기존 설치에서는 linger를 해제하지 않는다.

   ```sh
   ./result/bin/bukupan service uninstall
   ./result/bin/bukupan service install \
     --listen-host 127.0.0.1 --listen-port 3001 \
     --database /home/song/.local/share/bukupan/board.sqlite
   ./result/bin/bukupan service start
   curl --fail --silent --show-error http://127.0.0.1:3001/healthz

   ./result/bin/bukupan site install \
     --target "$HOME/.config/hooddy/sites.d/bukupan.caddy" \
     --domain bukupan.hooreique.com \
     --listen-host 127.0.0.1 --listen-port 3001
   "$HOME/.config/hooddy/caddy/bin/caddy" validate \
     --config "$HOME/.config/hooddy/Caddyfile" --adapter caddyfile
   systemctl --user reload-or-restart hooddy.service
   ```

   각 명령이 성공한 경우에만 다음 명령을 실행한다. 특히 백엔드 상태 검사나 Caddy 검증 실패를 무시하고 reload하지 않는다. CLI가 관리하지 않는 파일의 교체를 거절하면 원인을 확인하고, 강제로 삭제하거나 덮어쓰지 않는다. 기존 그림을 초기화하거나 개발 DB로 바꾸지 않는다.

4. **배포 확인.** 서비스 활성화·자동 시작, 외부 HTTPS와 정적 파일 응답을 확인한다. 브라우저를 새로고침해 기존 그림이 남아 있는지와 두 창 사이 그리기 동기화도 확인한다.

   ```sh
   systemctl --user is-active bukupan.service hooddy.service
   systemctl --user is-enabled bukupan.service
   curl --fail --silent --show-error https://bukupan.hooreique.com/healthz
   curl --fail --silent --show-error --output /dev/null https://bukupan.hooreique.com/
   journalctl --user -u bukupan.service -n 50 --no-pager
   ```

이 절차는 자동 롤백을 제공하지 않는다. 실패하면 실패한 단계와 서비스 로그를 확인하고 운영 DB와 백업을 보존한다. 이전 소스로 되돌릴 때도 해당 버전을 별도로 빌드한 뒤 같은 DB·주소로 서비스와 사이트를 함께 재설치하고 검증한다. DB 복원은 아래 복원 절차를 따른다. 배포 결과는 적용한 소스 버전, 외부 주소, 확인 결과와 백업 위치를 함께 보고한다.

## 저장과 백업

운영 문서는 기본적으로 `~/.local/share/bukupan/board.sqlite`에 보관한다. XDG 경로와 무관하게 고정하려면 다음처럼 지정한다. `service install`은 설치 시 결정한 절대 경로를 서비스 환경 파일에 기록한다. 서비스 제거·재설치는 그림 데이터를 삭제하지 않는다.

```sh
bukupan serve --database /path/to/board.sqlite
bukupan service install --database /path/to/board.sqlite --enable-linger
bukupan backup --database /path/to/board.sqlite --output /path/to/backups/board-2026-10-29.sqlite
```

백업은 실행 중인 서버에서도 일관된 SQLite 사본을 만든다. 출력 파일은 새 파일이어야 하며 부모 디렉터리는 미리 만들어 둔다. 기존 파일은 덮어쓰지 않는다. `--database`를 생략하면 기본 운영 문서를 사용하므로 별도 경로로 설치한 서비스는 백업에도 같은 경로를 지정한다. 주기적인 백업과 별도 장치 보관은 운영자가 설정한다.

복원은 서비스를 정지한 다음 백업을 **새 경로에 복사**하고, 그 경로를 `--database`로 지정해 실행한다. systemd 서비스는 제거 후 새 경로로 재설치하고 시작한다. 원본 문서와 백업은 확인이 끝날 때까지 보관한다. 실행 중인 DB 파일만 복사하거나 기존 DB 위에 사본을 덮지 않는다. WAL의 미반영 내용이나 이전 `-wal`·`-shm` 파일과 섞일 수 있다.

`nix run .#dev-server`는 운영 문서와 별개인 `$XDG_DATA_HOME/bukupan-dev/board.sqlite`(기본 `~/.local/share/bukupan-dev/board.sqlite`)를 사용한다. `BUKUPAN_DEV_DB=/path/to/dev.sqlite`로 바꿀 수 있다. 개발 서버를 다시 시작해도 이 문서는 남는다. 자동 검사는 매번 임시 DB를 사용한다.

화면의 `저장됨`은 서버 저장 확인을 뜻한다. 통신이 끊기면 새 편집을 멈추고 미전송 작업은 해당 브라우저에 보관해 재접속 때 보낸다. 브라우저 저장 실패 시에는 탭을 닫기 전에 `미전송 작업 내보내기`로 JSON을 보관할 수 있다. 서버의 그림 백업과 미전송 작업 내보내기는 별개다.

## 구 버전에서 전환

서버와 정적 클라이언트를 함께 갱신해야 한다. v1 탭은 새로고침하고, 기존 systemd 설치는 서비스 제거 후 새 CLI로 재설치한다. 사이트 파일은 새 `site install`로 갱신한 다음 master Caddy 설정을 검증·적용한다. CLI는 운영 중인 서비스나 Caddy를 자동 갱신하지 않는다.

10초 후 사라지던 메모리 그림은 이전 대상이 없다. 새 문서부터 영구 보관한다. 문서는 삭제하거나 취소해도 복구 이력을 유지하므로 디스크 사용량이 증가한다. 현재는 문서 하나·서버 프로세스 하나를 지원하고, 인증과 이력 압축·자동 백업 스케줄은 포함하지 않는다. 좌표·화면 밀도·작업 크기의 구체적인 상한과 테스트 정책은 [CONTRIBUTING.md](CONTRIBUTING.md)를 참고한다.
