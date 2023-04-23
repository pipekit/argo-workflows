{
  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixpkgs-unstable";
    flake-parts = { url = "github:hercules-ci/flake-parts"; inputs.nixpkgs-lib.follows = "nixpkgs"; };
    treefmt-nix.url = "github:numtide/treefmt-nix";
  };

  outputs = inputs:
    inputs.flake-parts.lib.mkFlake { inherit inputs; } {
      systems = [ "x86_64-linux" "x86_64-darwin" "aarch64-linux" "aarch64-darwin"];
      imports = [ inputs.treefmt-nix.flakeModule ];

      perSystem = { pkgs, lib, config, ... }:
        let
          src = lib.sourceFilesBySuffices inputs.self [ ".go" ".mod" ".sum" ];
          package = {
            name = "controller";
            version = "0.0.1";
          };
        in
        {
          packages = {
            ${package.name} = pkgs.buildGoModule {
              pname = package.name;
              inherit (package) version;
              inherit src;
              vendorSha256 = "sha256-E8BNOjKCZKCJOss7syZCxTeG6vpPDIaH5lBk9LrzkIc=";
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

            default = config.packages.${package.name};
          };

          devShells = {
            ${package.name} = pkgs.mkShell {
              inherit (package) name;
              inputsFrom = [ config.packages.${package.name} ];
              packages = with pkgs; [
                mockery
                gopls
                go
                python310
              ];
            };
            default = config.devShells.${package.name};
          };

          treefmt = {
            projectRootFile = "flake.nix";
            programs.nixpkgs-fmt.enable = true;
            programs.gofmt.enable = true;
          };
        };
    };
}
