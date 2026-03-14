# Multi-stage Dockerfile for Rust scraper
FROM rust:latest as base

WORKDIR /app

# Install system dependencies including Chromium for WebDriver
RUN apt-get update && apt-get install -y \
    curl \
    wget \
    gnupg \
    unzip \
    chromium \
    chromium-driver \
    xvfb \
    && rm -rf /var/lib/apt/lists/*

# Create symlink for compatibility
RUN ln -sf /usr/bin/chromium /usr/bin/google-chrome

# Copy dependency files first for caching
COPY Cargo.toml Cargo.lock ./

# Development stage
FROM base as development
# Create a dummy main.rs to build dependencies
RUN mkdir src && echo "fn main() {}" > src/main.rs
RUN cargo build --release
RUN rm -rf src

# Copy source code
COPY src ./src

# Create logs directory with proper permissions
RUN mkdir -p /app/logs && chmod 777 /app/logs

# Build for development (faster compilation)
RUN cargo build

EXPOSE 3001

CMD ["cargo", "run"]

# Production build stage
FROM base as builder
RUN mkdir src && echo "fn main() {}" > src/main.rs
RUN cargo build --release
RUN rm -rf src

COPY src ./src
RUN cargo build --release

# Production runtime stage
FROM debian:bookworm-slim as production

RUN apt-get update && apt-get install -y \
    ca-certificates \
    curl \
    chromium \
    chromium-driver \
    xvfb \
    && rm -rf /var/lib/apt/lists/*

# Create symlink for compatibility
RUN ln -sf /usr/bin/chromium /usr/bin/google-chrome

WORKDIR /app

# Create logs directory with proper permissions BEFORE switching user
RUN mkdir -p /app/logs && chmod 777 /app/logs

COPY --from=builder /app/target/release/rust-scraper /app/rust-scraper

EXPOSE 3001

USER 1000

CMD ["./rust-scraper"]