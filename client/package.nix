{ lib, runCommand }:
let
  src = lib.fileset.toSource {
    root = ./.;
    fileset = lib.fileset.difference ./. ./package.nix;
  };
in
runCommand "bukupan-client" { } ''
  mkdir -p "$out"
  cp ${src}/* "$out/"
''
