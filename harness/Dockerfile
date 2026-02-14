FROM golang:1.24-bookworm

# System packages (tmux for game/Claude sessions, jq for hook scripts)
RUN apt-get update && apt-get install -y --no-install-recommends \
    gcc libc6-dev tmux curl jq ca-certificates pkg-config \
    && rm -rf /var/lib/apt/lists/*

# Rust toolchain via rustup (in /usr/local so tmux sessions inherit it)
ENV RUSTUP_HOME=/usr/local/rustup \
    CARGO_HOME=/usr/local/cargo \
    PATH=/usr/local/cargo/bin:$PATH
RUN curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | \
    sh -s -- -y --default-toolchain stable --profile minimal && \
    rustup target add wasm32-unknown-unknown

# Cargo tools (slow to compile — separate layer for caching)
RUN cargo install trunk && \
    cargo install cargo-watch && \
    cargo install wasm-bindgen-cli --version 0.2.108

# Go dev tools (air for hot-reload, templ for template generation)
RUN go install github.com/air-verse/air@v1.62.0 && \
    go install github.com/a-h/templ/cmd/templ@v0.3.977

# Claude Code CLI (native installer — no Node.js needed)
RUN curl -fsSL https://claude.ai/install.sh | bash
ENV PATH="/root/.local/bin:$PATH"

COPY scripts/dev-entrypoint.sh /usr/local/bin/dev-entrypoint.sh
RUN chmod +x /usr/local/bin/dev-entrypoint.sh

WORKDIR /app/harness
EXPOSE 8080
EXPOSE 8081-8180
EXPOSE 9001-9100
ENTRYPOINT ["dev-entrypoint.sh"]
