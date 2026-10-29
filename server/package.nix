{ lib, buildGoModule }:
buildGoModule {
  pname = "bukupan-server";
  version = "0.1.0";
  src = lib.fileset.toSource {
    root = ../.;
    fileset = lib.fileset.unions [
      ../go.mod
      ../go.sum
      ../testdata
      ./.
    ];
  };
  vendorHash = "sha256-IUyZTlMqht5sx010M3kc96okTBsRrL1e+6m1VVWvK0k=";
  env.CGO_ENABLED = "1";
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
