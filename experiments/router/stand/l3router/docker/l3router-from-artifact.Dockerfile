# sing-box-l3router:local — pack linux/amd64 binary from `python run.py build` (or `all`).
# Avoids compiling inside Docker (toolchain/psiphon-tls match host; no proxy.golang.org from build container).
FROM alpine:3.21
RUN apk add --no-cache ca-certificates tzdata
COPY artifacts/sing-box-linux-amd64 /usr/local/bin/sing-box
RUN chmod +x /usr/local/bin/sing-box
ENTRYPOINT ["/usr/local/bin/sing-box"]
