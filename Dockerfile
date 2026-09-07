# Build image / 构建镜像
FROM golang:1.22-bookworm AS build

WORKDIR /xrockscache
COPY go.mod ./
COPY cmd ./cmd
COPY internal ./internal
RUN go build -trimpath -ldflags="-s -w" -o /out/xrockscache ./cmd/xrockscache

# Runtime image / 运行镜像
FROM debian:bookworm-slim

RUN apt-get update && \
    apt-get install -y --no-install-recommends ca-certificates redis-tools && \
    rm -rf /var/lib/apt/lists/* && \
    groupadd --gid=999 -r xrockscache && \
    useradd --uid=999 -r -g xrockscache xrockscache && \
    mkdir -p /var/lib/xrockscache && \
    chown -R xrockscache:xrockscache /var/lib/xrockscache

COPY --from=build /out/xrockscache /bin/xrockscache
COPY ./LICENSE ./NOTICE /xrockscache/
COPY ./xrockscache.conf /var/lib/xrockscache/xrockscache.conf

USER 999
VOLUME /var/lib/xrockscache
EXPOSE 6666

HEALTHCHECK --interval=30s --timeout=3s --start-period=10s --retries=3 \
    CMD redis-cli -h 127.0.0.1 -p 6666 PING | grep -E '(PONG|NOAUTH)' || exit 1

ENTRYPOINT ["xrockscache", "-c", "/var/lib/xrockscache/xrockscache.conf", "-dir", "/var/lib/xrockscache", "-bind", "0.0.0.0"]
