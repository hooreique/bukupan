{
  lib,
  stdenv,
  go,
  python3,
  nodejs,
  caddy,
  systemd,
  playwright-test,
  wayland,
}:
{
  base = [
    go
    python3
    nodejs
    caddy
    stdenv.cc
  ]
  ++ lib.optionals stdenv.hostPlatform.isLinux [ systemd ];

  # The runner wrapper supplies NODE_PATH and the matching browser revisions.
  browser = [ playwright-test ];

  browserEnvironment = lib.optionalAttrs stdenv.hostPlatform.isLinux {
    # SwiftShader dlopens libwayland-client; it is not an ELF dependency that
    # autoPatchelf can discover. Scope this search path to the browser process.
    BUKUPAN_BROWSER_LIBRARY_PATH = lib.makeLibraryPath [ wayland ];
  };

  environment = lib.optionalAttrs stdenv.hostPlatform.isLinux {
    SYSTEMD_UNIT_PATH = "${systemd}/example/systemd/user";
  };
}
