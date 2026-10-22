{
  runCommand,
  python3,
  caddy,
  go,
  nodejs,
  stdenv,
  lib,
  systemd,
}:
runCommand "bukupan-cli-tests"
  {
    nativeBuildInputs = [
      python3
      caddy
      go
      nodejs
      stdenv.cc
    ]
    ++ lib.optionals stdenv.hostPlatform.isLinux [ systemd ];
  }
  ''
        cp -r ${../.} source
        chmod -R u+w source
        cd source
        export HOME=$TMPDIR
    ${lib.optionalString stdenv.hostPlatform.isLinux "export SYSTEMD_UNIT_PATH=${systemd}/example/systemd/user"}
        go build -o "$TMPDIR/bukupan-server" ./server
        export BUKUPAN_TEST_SERVER="$TMPDIR/bukupan-server"
        python3 -m unittest discover -s cli -v
        node scripts/test-codec.js
        go test ./...
        go test -race ./...
        touch "$out"
  ''
