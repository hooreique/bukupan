{
  writeShellApplication,
  caddy,
  coreutils,
  backendPackage,
  clientPackage,
}:
writeShellApplication {
  name = "bukupan-dev-server";
  runtimeInputs = [
    backendPackage
    caddy
    coreutils
  ];
  text = ''
    export BUKUPAN_CLIENT_ROOT="''${BUKUPAN_CLIENT_ROOT:-${clientPackage}}"
    ${builtins.readFile ../scripts/dev-server.sh}
  '';
}
