{
  description = "부끄판 — a shared, fading canvas";

  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";

  outputs =
    { nixpkgs, ... }:
    let
      forAllSys =
        perSys:
        nixpkgs.lib.genAttrs [ "aarch64-darwin" "aarch64-linux" "x86_64-linux" ] (
          system: perSys nixpkgs.legacyPackages.${system}
        );
    in
    {
      packages = forAllSys (
        pkgs:
        let
          server = pkgs.buildGoModule {
            pname = "bukupan-server";
            version = "0.1.0";
            src = pkgs.lib.fileset.toSource {
              root = ./.;
              fileset = pkgs.lib.fileset.unions [
                ./go.mod
                ./server
              ];
            };
            vendorHash = null;
            env.CGO_ENABLED = "0";
            subPackages = [ "server" ];
            ldflags = [
              "-s"
              "-w"
            ];
            postInstall = ''
              mv "$out/bin/server" "$out/bin/bukupan-server"
            '';
            meta = {
              mainProgram = "bukupan-server";
              license = pkgs.lib.licenses.mit;
              platforms = [
                "aarch64-darwin"
                "aarch64-linux"
                "x86_64-linux"
              ];
            };
          };

          client = pkgs.runCommand "bukupan-client" { } ''
            mkdir -p "$out"
            cp ${./client}/* "$out/"
          '';
          cli = pkgs.callPackage ./cli/package.nix {
            backendPackage = server;
            clientPackage = client;
          };
        in
        {
          default = cli;
          bukupan = cli;
          bukupan-server = server;
          bukupan-client = client;
          inherit client;

          dev-server = pkgs.writeShellApplication {
            name = "bukupan-dev-server";
            runtimeInputs = [
              server
              pkgs.caddy
              pkgs.coreutils
            ];
            text = ''
              export BUKUPAN_CLIENT_ROOT="''${BUKUPAN_CLIENT_ROOT:-${client}}"
              ${builtins.readFile ./scripts/dev-server.sh}
            '';
          };

        }
      );

      checks = forAllSys (pkgs: {
        cli =
          pkgs.runCommand "bukupan-cli-tests"
            {
              nativeBuildInputs = [
                pkgs.python3
                pkgs.caddy
                pkgs.go
                pkgs.stdenv.cc
              ]
              ++ pkgs.lib.optionals pkgs.stdenv.hostPlatform.isLinux [ pkgs.systemd ];
            }
            ''
                  cp -r ${./.} source
                  chmod -R u+w source
                  cd source
                  export HOME=$TMPDIR
              ${pkgs.lib.optionalString pkgs.stdenv.hostPlatform.isLinux "export SYSTEMD_UNIT_PATH=${pkgs.systemd}/example/systemd/user"}
                  go build -o "$TMPDIR/bukupan-server" ./server
                  export BUKUPAN_TEST_SERVER="$TMPDIR/bukupan-server"
                  python3 -m unittest discover -s cli -v
                  go test ./...
                  go test -race ./...
                  touch "$out"
            '';
      });

      devShells = forAllSys (pkgs: {
        default = pkgs.mkShell {
          packages = [
            pkgs.go
            pkgs.python3
            pkgs.caddy
            pkgs.nixfmt
          ];
        };
      });
    };
}
