FROM golang:1.22-bookworm

RUN apt-get update && \
    apt-get install -y --no-install-recommends git curl redis-tools vim gdb && \
    rm -rf /var/lib/apt/lists/*
