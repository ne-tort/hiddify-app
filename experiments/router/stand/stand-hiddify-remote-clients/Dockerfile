FROM ghcr.io/sagernet/sing-box:latest
RUN apk add --no-cache \
    ca-certificates iproute2 curl iputils iptables \
    samba samba-client python3 \
    && update-ca-certificates
COPY entrypoint.sh /entrypoint.sh
RUN sed -i 's/\r$//' /entrypoint.sh
ENTRYPOINT ["/bin/sh", "/entrypoint.sh"]
