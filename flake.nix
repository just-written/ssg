{
    description = "SSG Dev Flake";
    inputs = { nixpkgs.url = "github:nixos/nixpkgs/nixos-unstable"; };
    outputs = { nixpkgs, ... }: let
        system = "x86_64-linux";
    pkgs = nixpkgs.legacyPackages.${system};
    in {
        devShells.${system}.default = pkgs.mkShell {
            nativeBuildInputs = with pkgs; [ 
                go gopls vscode-langservers-extracted 
                wrangler
            ];
        };
    };
}
