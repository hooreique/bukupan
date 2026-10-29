{
  mkShell,
  lib,
  callPackage,
  nixfmt,
  withBrowser ? false,
}:
let
  tools = callPackage ./nix/test-tools.nix { };
in
mkShell (
  tools.environment
  // lib.optionalAttrs withBrowser tools.browserEnvironment
  // {
    packages = tools.base ++ [ nixfmt ] ++ lib.optionals withBrowser tools.browser;
  }
)
