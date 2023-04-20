{ pkgs ? import (fetchTarball "https://github.com/NixOS/nixpkgs/archive/06278c77b5d162e62df170fec307e83f1812d94b.tar.gz") {}
}:

let 
  pkgs = import<nixpkgs> {};
  pythonEnv = pkgs.python310.withPackages (ps: [
    ps.pytest
    ps.typing-extensions
    ps.mypy
    ps.autopep8
    ps.pip   
  ]);
  nodejs-19_x = pkgs.nodejs-19_x;
  yarn = pkgs.yarn.override { inherit nodejs-19_x; };
in 
# All go builds go here
let 
  workflow-controller = pkgs.buildGoModule rec {
    pname = "workflow-controller";
    version = "0.0.1";
    src = builtins.filterSource
            (path: type: path != "default.nix")
            ./.; # avoid recompile when default.nix changes
    vendorHash = null;
    doCheck = false;
  };

  mockery = pkgs.buildGoModule rec {
    pname = "mockery";
    version = "2.10.0";

    src = pkgs.fetchFromGitHub {
      owner = "vektra";
      repo = "mockery";
      rev = "v${version}";
      sha256 = "sha256-udzBhCkESd/5GEJf9oVz0nAQDmsk4tenvDP6tbkBIao=";
    }; 
    doCheck = false;
    vendorHash =  "sha256-iuQx2znOh/zsglnJma7Y4YccVArSFul/IOaNh449SpA=";
  };

  protoc-gen-gogo-all = pkgs.buildGoModule rec {
    pname = "protoc-gen-gogo";
    version = "1.3.2";

    src = pkgs.fetchFromGitHub {
      owner = "gogo";
      repo = "protobuf";
      rev = "v${version}";
      sha256 = "sha256-CoUqgLFnLNCS9OxKFS7XwjE17SlH6iL1Kgv+0uEK2zU=";
    };
    doCheck = false; 
    vendorHash = "sha256-nOL2Ulo9VlOHAqJgZuHl7fGjz/WFAaWPdemplbQWcak=";
  };

  grpc-ecosystem = pkgs.buildGoModule rec {
    pname = "grpc-ecosystem";
    version = "1.16.0";

    src = pkgs.fetchFromGitHub {
      owner = "grpc-ecosystem";
      repo = "grpc-gateway";
      rev = "v${version}";
      sha256 = "sha256-jJWqkMEBAJq50KaXccVpmgx/hwTdKgTtNkz8/xYO+Dc=";
    };
    doCheck = false; 
    vendorHash = "sha256-jVOb2uHjPley+K41pV+iMPNx67jtb75Rb/ENhw+ZMoM=";
  };

  go-swagger = pkgs.buildGoModule rec {
    pname = "go-swagger";
    version = "0.28.0";

    src = pkgs.fetchFromGitHub {
      owner = "go-swagger";
      repo = "go-swagger";
      rev = "v${version}";
      sha256 = "sha256-Bw84HQxrI8cSBEM1cxXmWCPqKZa5oGsob2iuUsiAZ+A=";
    };
    doCheck = false;
    vendorHash = "sha256-lhb3tvwhTPNo5+OhGgc3p3ddxFtL1gaIVTpZw0krBhM=";
  };

  controller-tools = pkgs.buildGoModule rec {
    pname = "controller-tools";
    version = "0.4.1";

    src = pkgs.fetchFromGitHub {
      owner = "kubernetes-sigs";
      repo = "controller-tools";
      rev = "v${version}";
      sha256 = "sha256-NQlSP9hRLXr+iZo0OeyF1MQs3PourQZN0I0v4Wv5dkE=";
    };
    vendorHash = "sha256-89hzPiqP++tQpPkcSvzc1tHxHcj5PI71RxxxUCgm0BI=";
    doCheck = false;
  };

  k8sio-tools = pkgs.buildGoModule rec {
    pname = "k8sio-tools";
    version = "0.21.5";

    src = pkgs.fetchFromGitHub {
      owner = "kubernetes";
      repo = "code-generator";
      rev = "v${version}";
      sha256 = "sha256-x05eAO2oAq/jm1SBgwjKo6JRt/j4eMn7oA0cwswLxk8=";
    };
    vendorHash = "sha256-Re8Voj2nO8geLCrbDPqD5rLyiUqE7APcgOnAEJzlkOk=";
    doCheck = false;
  };

  goreman = pkgs.buildGoModule rec {
    pname = "goreman";
    version = "0.3.11";
    src = pkgs.fetchFromGitHub {
      owner = "mattn";
      repo = "goreman";
      rev = "v${version}";
      sha256 = "sha256-TbJfeU94wakI2028kDqU+7dRRmqXuqpPeL4XBaA/HPo=";
    };
    vendorHash = "sha256-87aHBRWm5Odv6LeshZty5N31sC+vdSwGlTYhk3BZkPo=";
    doCheck = false;
  };

  stern = pkgs.buildGoModule rec {
    pname = "stern";
    version = "1.25.0";
    src = pkgs.fetchFromGitHub {
      owner = "stern";
      repo = "stern";
      rev = "v${version}";
      sha256 = "sha256-E4Hs9FH+6iQ7kv6CmYUHw9HchtJghMq9tnERO2rpL1g=";
    };
    vendorHash = "sha256-+B3cAuV+HllmB1NaPeZitNpX9udWuCKfDFv+mOVHw2Y=";
    doCheck = false;
  };

  staticfiles = pkgs.buildGoModule rec {
    pname = "staticfiles";
    version = "0.0.1"; # no official version
    src = pkgs.fetchFromGitHub {
      owner = "isubasinghe";
      repo = "staticfiles";
      rev = "3d5ddde4d52ddef391b5f4f37e06c80980a5c0c2";
      sha256 = "sha256-fyamqYhKXnY0fzNhS3SL15yHDA/pIuoQ+NsehfA7BCE=";
    };
    vendorHash = null;
  };
in
# this is the glue code to above deps
pkgs.stdenv.mkDerivation {
  pname = "controller";
  version = "0.0.1";

  src = ./.;

  shellHook = ''
    unset GOPATH;
    alias mkdocs="./venv/bin/python -m mkdocs";
  '';
  nativeBuildInputs = [];

  buildInputs = [
      pkgs.gcc 
      (pkgs.haskellPackages.ghcWithPackages (ps: [ ps.shake ]))
      pkgs.go
      nodejs-19_x
      pkgs.yarn
      pkgs.clang-tools_13
      pkgs.jq
      pkgs.protobuf3_20
      pkgs.docker
      pkgs.kube3d
      pkgs.git
      pkgs.kubectl
      workflow-controller
      mockery
      protoc-gen-gogo-all
      grpc-ecosystem
      go-swagger
      controller-tools
      k8sio-tools
      goreman
      stern
      staticfiles
      pythonEnv
    ];

  buildPhase = '' 
    python -m venv ./venv;
    ./venv/bin/pip install -r requirements.txt;
  '';

  installPhase = ''
    mkdir $out
    cp -r _build/* $out
  '';
}


