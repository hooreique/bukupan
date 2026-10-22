{
  backendPackage,
  clientPackage,
  python3,
  makeWrapper,
  symlinkJoin,
  nix,
  systemd,
  stdenv,
  lib,
}:
let
  script = python3.pkgs.buildPythonApplication {
    pname = "bukupan-cli";
    version = "0.1.0";
    format = "other";
    src = ./.;
    dontBuild = true;
    installPhase = ''
      mkdir -p $out/bin
      cp bukupan.py $out/bin/bukupan
      chmod +x $out/bin/bukupan
      substituteInPlace $out/bin/bukupan \
        --replace-fail '@server@' '${backendPackage}' \
        --replace-fail '@client@' '${clientPackage}' \
        --replace-fail '@template@' '${./Caddyfile.template}'
    '';
  };
in
symlinkJoin {
  name = "bukupan";
  paths = [ script ];
  nativeBuildInputs = [ makeWrapper ];
  postBuild = ''
    wrapProgram $out/bin/bukupan --prefix PATH : ${
      lib.makeBinPath ([ nix ] ++ lib.optionals stdenv.hostPlatform.isLinux [ systemd ])
    }
  '';
  meta.mainProgram = "bukupan";
}
