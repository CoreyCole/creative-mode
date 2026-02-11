default:
    @just --list

harness:
    cd harness && just dev

setup:
    ./scripts/setup.sh
