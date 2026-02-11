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

setup:
    ./scripts/setup.sh

sync-thoughts:
    ./scripts/sync-thoughts.sh
