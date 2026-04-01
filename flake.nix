{
    description = "SSG Flake";
    inputs = {
        nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
        flake-utils.url = "github:numtide/flake-utils";
    };
    outputs = { self, nixpkgs, flake-utils }:
    flake-utils.lib.eachDefaultSystem (system: let
        pkgs = nixpkgs.legacyPackages.${system};
    in {
        packages.default = pkgs.buildGoModule {
            pname = "ssg";
            version = "0.0.2";
            src = ./.;
            vendorHash = "sha256-RTAXkeC3K4V1E5qNuj51LKQLImh/IkZeihN3Qls7Y8Q=";
        };

        apps.default = {
            type = "app";
            program = "${self.packages.${system}.default}/bin/ssg";
        };

        devShells.default = pkgs.mkShell {
            buildInputs = with pkgs; [
                go gopls
            ];
        };
    });
}
