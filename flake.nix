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

            # Docker (CLI only — Docker Engine installed by bootstrap script)
            docker
            docker-compose
          ];

          shellHook = ''
            # Set up oh-my-zsh if not already configured
            export ZSH="${pkgs.oh-my-zsh}/share/oh-my-zsh"
            export ZSH_THEME="robbyrussell"
            export plugins=(git fzf docker docker-compose)

            # Source oh-my-zsh
            if [ -f "$ZSH/oh-my-zsh.sh" ]; then
              source "$ZSH/oh-my-zsh.sh"
            fi

            # Enable fzf keybindings and completion
            if [ -n "$(command -v fzf)" ]; then
              source "${pkgs.fzf}/share/fzf/key-bindings.zsh" 2>/dev/null
              source "${pkgs.fzf}/share/fzf/completion.zsh" 2>/dev/null
            fi
          '';
        };
      }
    );
}
