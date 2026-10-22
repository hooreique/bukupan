{
  mkShell,
  go,
  python3,
  caddy,
  nixfmt,
}:
mkShell {
  packages = [
    go
    python3
    caddy
    nixfmt
  ];
}
