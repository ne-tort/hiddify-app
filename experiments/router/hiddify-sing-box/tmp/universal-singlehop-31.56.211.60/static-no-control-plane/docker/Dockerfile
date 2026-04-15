# sing-box with l3router and clash API (same tags as AGENTS.md Linux build).
# Build from repository root: experiments/router/hiddify-sing-box
#
#   docker build -f tmp/universal-singlehop-31.56.211.60/static-no-control-plane/docker/Dockerfile -t sing-box-l3router:local .

FROM golang:1.24-bookworm AS build
WORKDIR /src
COPY . .
ENV CGO_ENABLED=0
RUN GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" \
    -tags with_gvisor,with_clash_api,with_utls \
    -o /out/sing-box ./cmd/sing-box

FROM alpine:3.21
RUN apk add --no-cache ca-certificates tzdata
COPY --from=build /out/sing-box /usr/local/bin/sing-box
ENTRYPOINT ["/usr/local/bin/sing-box"]
