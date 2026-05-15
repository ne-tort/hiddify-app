# syntax=docker/dockerfile:1.7

FROM --platform=$BUILDPLATFORM node:alpine AS front-builder
WORKDIR /app
COPY frontend/package.json frontend/package-lock.json frontend/.npmrc ./
RUN --mount=type=cache,id=s-ui-npm-cache,target=/root/.npm \
    npm install --prefer-offline --no-audit
COPY frontend/ ./
RUN npm run build

FROM golang:1.25-alpine AS backend-builder
WORKDIR /app
ARG TARGETARCH
ARG TARGETVARIANT
ENV CGO_ENABLED=1
ENV CGO_CFLAGS="-D_LARGEFILE64_SOURCE"
ENV GOARCH=$TARGETARCH

RUN apk update && apk add --no-cache \
    gcc \
    musl-dev \
    libc-dev \
    make \
    git \
    wget \
    unzip \
    bash \
    curl

ENV CC=gcc

# libcronet is fetched on the host by `run.py` before `docker compose build` (BuildKit DNS to GitHub assets is flaky on some hosts).
COPY libcronet-linux-${TARGETARCH}.so ./libcronet.so
RUN chmod 755 ./libcronet.so && test -s ./libcronet.so

# Prime Go module cache using only dependency manifests first.
COPY --from=core /go.mod /hiddify-core/hiddify-sing-box/go.mod
COPY --from=core /go.sum /hiddify-core/hiddify-sing-box/go.sum
COPY --from=core /replace /hiddify-core/hiddify-sing-box/replace
COPY go.mod go.sum ./

COPY . .
COPY --from=core / /hiddify-core/hiddify-sing-box/
COPY --from=front-builder /app/dist/ /app/web/html/

# Hard check that sing-box is resolved from local hiddify fork.
RUN test -d /hiddify-core/hiddify-sing-box/protocol/l3router && \
    RESOLVED_DIR="$(go list -m -f '{{.Dir}}' github.com/sagernet/sing-box)" && \
    echo "Resolved sing-box dir: ${RESOLVED_DIR}" && \
    test "${RESOLVED_DIR}" = "/hiddify-core/hiddify-sing-box"

# Build tags: sync with hiddify-core `go run ./cmd/print_core_build_tags -windows` (base + with_purego for libcronet + naive on Linux).
RUN --mount=type=cache,id=s-ui-go-mod-cache,target=/go/pkg/mod \
    --mount=type=cache,id=s-ui-go-build-cache,target=/root/.cache/go-build \
    if [ "$TARGETARCH" = "arm" ]; then export GOARM=7; [ "$TARGETVARIANT" = "v6" ] && export GOARM=6; fi; \
    go build -ldflags="-w -s -checklinkname=0" \
    -tags "with_gvisor,with_quic,with_wireguard,with_awg,with_l3router,with_utls,with_clash_api,with_grpc,with_acme,with_naive_outbound,with_conntrack,badlinkname,tfogo_checklinkname0,with_tailscale,ts_omit_logtail,ts_omit_ssh,ts_omit_drive,ts_omit_taildrop,ts_omit_webclient,ts_omit_doctor,ts_omit_capture,ts_omit_kube,ts_omit_aws,ts_omit_synology,ts_omit_bird,with_purego,with_masque" \
    -o sui main.go

FROM alpine
LABEL org.opencontainers.image.authors="alireza7@gmail.com"
ENV TZ=Asia/Tehran
WORKDIR /app
RUN set -ex && apk add --no-cache --upgrade bash tzdata ca-certificates nftables iproute2 openssl
COPY --from=backend-builder /app/sui /app/libcronet.so /app/
COPY entrypoint.sh /app/
RUN sed -i 's/\r$//' /app/entrypoint.sh && chmod +x /app/entrypoint.sh
ENTRYPOINT [ "./entrypoint.sh" ]