default:
    @just --list

# Verify everything compiles (Go harness + Rust server + Rust WASM client)
check:
    ./scripts/check.sh

# Format all code (Go + Rust)
fmt:
    ./scripts/fmt.sh

harness:
    cd harness && just dev

# Install playwright-cli for autonomous browser debugging
setup-playwright:
    npm install -g @playwright/cli@latest
    playwright-cli install

setup:
    ./scripts/setup.sh
    just setup-playwright

# Debug: query world game state (run `just debug --help` for commands)
debug worldID +args='--help':
    ./scripts/debug.sh {{worldID}} {{args}}

sync-thoughts:
    ./scripts/sync-thoughts.sh

# Marketing site
site-up:
    cd site && just up

site-down:
    cd site && just down
