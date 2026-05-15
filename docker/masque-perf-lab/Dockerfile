# sing-box (linux/amd64) + iperf3 + socat — образ для MASQUE perf lab (см. AGENTS.md §16).
# Бинарник кладётся в artifacts/sing-box-linux-amd64 (скрипт scripts/Build-MasquePerfLab.ps1).
#
# База Ubuntu 24.04 (apt iperf3 3.16): Alpine apk даёт iperf 3.17.x — UDP ctrl JSON не договаривается с типичным
# сервером apt 3.16 на эталонном хосте (TCP iperf ок, UDP probe — "unable to read from stream socket").
FROM ubuntu:24.04
ENV DEBIAN_FRONTEND=noninteractive
RUN apt-get update \
  && apt-get install -y --no-install-recommends \
    ca-certificates tzdata openssl iproute2 bash socat curl iperf3 dnsutils \
  && rm -rf /var/lib/apt/lists/* \
  && iperf3 -v 2>&1 | grep -qE 'iperf 3\.1[46]' \
  && command -v dig >/dev/null
COPY artifacts/sing-box-linux-amd64 /usr/local/bin/sing-box
ARG SINGBOX_ARTIFACT_STAMP=0
RUN chmod +x /usr/local/bin/sing-box \
  && echo "${SINGBOX_ARTIFACT_STAMP:-0}" > /tmp/.sing-box-artifact-stamp
