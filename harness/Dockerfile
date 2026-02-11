FROM golang:1.24-bookworm

# gcc + libc for CGo (mattn/go-sqlite3)
RUN apt-get update && apt-get install -y --no-install-recommends \
    gcc \
    libc6-dev \
    && rm -rf /var/lib/apt/lists/*

# Copy entrypoint
COPY scripts/dev-entrypoint.sh /usr/local/bin/dev-entrypoint.sh
RUN chmod +x /usr/local/bin/dev-entrypoint.sh

WORKDIR /app/harness

EXPOSE 8080

ENTRYPOINT ["dev-entrypoint.sh"]
