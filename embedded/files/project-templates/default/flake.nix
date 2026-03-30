{
  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    ssg.url = "github:just-written/ssg";
  };
  outputs = { nixpkgs, ssg, ... }: let
    system = "x86_64-linux"; # adjust as needed
    pkgs = nixpkgs.legacyPackages.${system};
  in {
    devShells.${system}.default = pkgs.mkShell {
      nativeBuildInputs = [
        pkgs.wrangler
        ssg.packages.${system}.default
      ];
    };
  };
}
