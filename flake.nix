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
          server = pkgs.callPackage ./server/package.nix { };
          client = pkgs.callPackage ./client/package.nix { };
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

          dev-server = pkgs.callPackage ./nix/dev-server.nix {
            backendPackage = server;
            clientPackage = client;
          };
        }
      );

      checks = forAllSys (pkgs: {
        cli = pkgs.callPackage ./nix/checks.nix { };
      });

      devShells = forAllSys (pkgs: {
        default = pkgs.callPackage ./shell.nix { };
      });
    };
}
