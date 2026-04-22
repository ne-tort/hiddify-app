# Официальный образ Hiddify Core (внутри — форк sing-box + CLI). Поверх — sysctl/SMB как в старом стенде.
FROM ghcr.io/hiddify/hiddify-core:latest
RUN apk add --no-cache \
    ca-certificates iproute2 curl iputils iptables \
    samba samba-client python3 \
    && update-ca-certificates
COPY entrypoint.sh /entrypoint.sh
RUN sed -i 's/\r$//' /entrypoint.sh
ENTRYPOINT ["/bin/sh", "/entrypoint.sh"]
