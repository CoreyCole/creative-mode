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
        # Installable package: nix profile install /home/deploy/creative-mode
        # Puts all dev tools into ~/.nix-profile/bin/ (available system-wide)
        packages.default = pkgs.buildEnv {
          name = "creative-mode-tools";
          paths = with pkgs; [
            just git curl jq sqlite
            go_1_24 golangci-lint gcc pkg-config
            openssl.dev
            tmux sqlc nodejs_22
            (python3.withPackages (ps: with ps; [
              mdformat
              mdformat-frontmatter
            ]))
            uv bubblewrap temporal-cli
          ];
        };

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
            golangci-lint
            gcc
            pkg-config
            openssl.dev

            # Runtime (game server tmux sessions)
            tmux

            # Code generation
            sqlc

            # Node.js (playwright-cli for autonomous world testing)
            nodejs_22

            # Python + uv (Claude Code hook scripts use `uv run`, debug.sh JSON processing)
            (python3.withPackages (ps: with ps; [
              mdformat
              mdformat-frontmatter
            ]))
            uv
          ];

          shellHook = "";
        };
      }
    );
}
