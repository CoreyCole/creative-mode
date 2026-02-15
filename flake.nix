{
  description = "Creative Mode server environment";

  inputs = {
    nixpkgs.url = "github:nixos/nixpkgs/nixos-unstable";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs = { self, nixpkgs, flake-utils }:
    flake-utils.lib.eachSystem [ "aarch64-linux" "x86_64-linux" ] (system:
      let
        pkgs = nixpkgs.legacyPackages.${system};
      in {
        devShells.default = pkgs.mkShell {
          packages = with pkgs; [
            # Shell
            zsh
            oh-my-zsh
            fzf

            # Dev tools
            just
            git
            curl
            jq
            sqlite

            # Build tools (Go harness compilation)
            go_1_24
            gcc
            pkg-config

            # Runtime (game server tmux sessions, tailwind)
            tmux
            nodejs_22
            pnpm

            # Code generation
            sqlc

            # Docker (CLI only — Docker Engine installed by bootstrap script)
            docker
            docker-compose
          ];

          # oh-my-zsh is configured in the login shell (.zshrc) — not here.
          # direnv evaluates shellHook inside bash, so sourcing oh-my-zsh
          # fails. The flake still provides fzf/zsh as packages for PATH.
          shellHook = "";
        };
      }
    );
}
