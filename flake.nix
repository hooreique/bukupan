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
            pname = "bukupan";
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
              mv "$out/bin/server" "$out/bin/bukupan"
            '';
            meta = {
              mainProgram = "bukupan";
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
        in
        {
          default = server;
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

          install = pkgs.writeShellApplication {
            name = "bukupan-install";
            runtimeInputs = [ pkgs.coreutils ];
            text = ''
              export BUKUPAN_CLIENT_PACKAGE=${client}
              ${builtins.readFile ./scripts/install.sh}
            '';
          };
        }
      );

      devShells = forAllSys (pkgs: {
        default = pkgs.mkShell {
          packages = [
            pkgs.go
            pkgs.caddy
            pkgs.nixfmt
          ];
        };
      });
    };
}
