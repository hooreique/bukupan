{
  runCommand,
  callPackage,
  lib,
  stdenv,
}:
let
  tools = callPackage ./test-tools.nix { };
  source = lib.fileset.toSource {
    root = ../.;
    fileset = lib.fileset.unions [
      ../go.mod
      ../server
      ../client
      ../cli
      ../scripts
      ../tests
      ../testdata
    ];
  };
  check =
    name: target: browser:
    runCommand "bukupan-tests-${name}"
      (
        tools.environment
        // lib.optionalAttrs browser tools.browserEnvironment
        // lib.optionalAttrs stdenv.hostPlatform.isDarwin {
          __darwinAllowLocalNetworking = true;
        }
        // {
          nativeBuildInputs = tools.base ++ lib.optionals browser tools.browser;
          CI = "1";
        }
      )
      ''
        cp -r ${source} source
        chmod -R u+w source
        cd source
        export GOCACHE="$TMPDIR/go-cache"
        export GOPATH="$TMPDIR/go"
        export XDG_CACHE_HOME="$TMPDIR/cache"
        python3 scripts/test ${target}
        touch "$out"
      '';
in
{
  unit = check "unit" "fast" false;
  race = check "race" "race" false;
  integration = check "integration" "integration" false;
  browser = check "browser" "browser" true;
}
