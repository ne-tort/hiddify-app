#!/usr/bin/env python3
"""Derive REALITY public key (URL-safe base64) from server private key."""
import base64
import sys

try:
    from cryptography.hazmat.primitives.asymmetric.x25519 import X25519PrivateKey
except ImportError:
    print("need: pip install cryptography", file=sys.stderr)
    sys.exit(1)

def main() -> None:
    sk_b64 = sys.argv[1] if len(sys.argv) > 1 else ""
    if not sk_b64:
        print("usage: derive_reality_public.py <private_key_b64>", file=sys.stderr)
        sys.exit(2)
    pad = "=" * (-len(sk_b64) % 4)
    sk = base64.urlsafe_b64decode(sk_b64 + pad)
    pk = X25519PrivateKey.from_private_bytes(sk).public_key().public_bytes_raw()
    out = base64.urlsafe_b64encode(pk).decode().rstrip("=")
    print(out)

if __name__ == "__main__":
    main()
