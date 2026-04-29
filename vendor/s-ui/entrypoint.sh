#!/bin/sh

set -e

TLS_CERT_PATH="${SUI_WEB_TLS_CERT:-${SUI_TLS_FALLBACK_CERT:-/app/cert/fullchain.pem}}"
TLS_KEY_PATH="${SUI_WEB_TLS_KEY:-${SUI_TLS_FALLBACK_KEY:-/app/cert/privkey.pem}}"

if [ "${SUI_TLS_SELF_SIGNED:-}" = "1" ] || [ "${SUI_TLS_SELF_SIGNED:-}" = "true" ]; then
    if [ ! -f "$TLS_CERT_PATH" ] || [ ! -f "$TLS_KEY_PATH" ]; then
        CERT_DIR="$(dirname "$TLS_CERT_PATH")"
        KEY_DIR="$(dirname "$TLS_KEY_PATH")"
        mkdir -p "$CERT_DIR" "$KEY_DIR"
        DAYS="${SUI_TLS_SELF_SIGNED_DAYS:-36500}"
        CN="${SUI_TLS_SELF_SIGNED_CN:-localhost}"
        openssl req -x509 -nodes -newkey rsa:4096 -sha256 \
            -days "$DAYS" \
            -subj "/CN=${CN}" \
            -keyout "$TLS_KEY_PATH" \
            -out "$TLS_CERT_PATH"
        chmod 600 "$TLS_KEY_PATH"
        chmod 644 "$TLS_CERT_PATH"
    fi
fi

export SUI_WEB_TLS_CERT="$TLS_CERT_PATH"
export SUI_WEB_TLS_KEY="$TLS_KEY_PATH"

DB_PATH="${SUI_DB_FOLDER:-/app/db}/s-ui.db"
if [ -f "$DB_PATH" ]; then
	./sui migrate
fi

exec ./sui