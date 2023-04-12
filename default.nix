let 
  pkgs = import<nixpkgs> {};
  pythonEnv = pkgs.python310.withPackages (ps: [
    ps.pytest
    ps.typing-extensions
    ps.mypy
    ps.autopep8
  ]);
in 
pkgs.stdenv.mkDerivation {
  pname = "controller";
  version = "0.0.1";

  src = ./.;

  nativeBuildInputs = [];

  buildInputs = [
      pkgs.gcc 
      (pkgs.haskellPackages.ghcWithPackages (ps: [ ps.shake ]))
      pkgs.go
      pkgs.nodejs-19_x
      pythonEnv
    ];

  buildPhase = '' 
  '';

  installPhase = ''
    mkdir $out
    cp -r _build/* $out
  '';
}


