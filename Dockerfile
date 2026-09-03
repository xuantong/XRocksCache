# Build image / 构建镜像
FROM debian:trixie-slim AS build

ARG MORE_BUILD_ARGS
ENV DEBIAN_FRONTEND=noninteractive

RUN apt-get update && \
    apt-get upgrade -y && \
    apt-get -y --no-install-recommends install \
      git build-essential autoconf cmake ninja-build libtool python3 libssl-dev clang ca-certificates && \
    apt-get autoremove -y && \
    apt-get clean && \
    rm -rf /var/lib/apt/lists/*

WORKDIR /xrockscache

COPY . .
RUN cmake -S . -B build -G Ninja \
      -DCMAKE_EXE_LINKER_FLAGS="-latomic" \
      -DENABLE_OPENSSL=ON \
      -DPORTABLE=1 \
      -DCMAKE_BUILD_TYPE=Release \
      ${MORE_BUILD_ARGS} && \
    cmake --build build --target xrockscache -j "$(nproc)"

# Runtime image / 运行镜像
FROM debian:trixie-slim

RUN apt-get update && \
    apt-get upgrade -y && \
    apt-get -y --no-install-recommends install openssl ca-certificates redis-tools binutils && \
    apt-get clean && \
    rm -rf /var/lib/apt/lists/*

RUN groupadd --gid=999 -r xrockscache && \
    useradd --uid=999 -r -g xrockscache xrockscache && \
    mkdir -p /var/run/xrockscache /var/lib/xrockscache && \
    chown -R xrockscache:xrockscache /var/run/xrockscache /var/lib/xrockscache

USER 999

VOLUME /var/lib/xrockscache

COPY --from=build /xrockscache/build/xrockscache /bin/xrockscache
COPY ./LICENSE ./NOTICE ./licenses /xrockscache/
COPY ./xrockscache.conf /var/lib/xrockscache/xrockscache.conf

ENV MALLOC_CONF="prof:true,prof_active:false,background_thread:true"

EXPOSE 6666

HEALTHCHECK --interval=30s --timeout=3s --start-period=30s --retries=3 \
    CMD redis-cli -p 6666 PING | grep -E '(PONG|NOAUTH)' || exit 1

ENTRYPOINT ["xrockscache", "-c", "/var/lib/xrockscache/xrockscache.conf", "--dir", "/var/lib/xrockscache", "--pidfile", "/var/run/xrockscache/xrockscache.pid", "--bind", "0.0.0.0"]
