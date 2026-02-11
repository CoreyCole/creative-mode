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

sync-thoughts:
    ./scripts/sync-thoughts.sh
