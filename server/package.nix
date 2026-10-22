{ lib, buildGoModule }:
buildGoModule {
  pname = "bukupan-server";
  version = "0.1.0";
  src = lib.fileset.toSource {
    root = ../.;
    fileset = lib.fileset.unions [
      ../go.mod
      ./.
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
    license = lib.licenses.mit;
    platforms = [
      "aarch64-darwin"
      "aarch64-linux"
      "x86_64-linux"
    ];
  };
}
