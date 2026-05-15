#!/usr/bin/env python3
"""
Rewrite stand MASQUE JSON from legacy flat TLS to sing-box-shaped tls / outbound_tls.

- Server endpoints: certificate + key -> tls.{certificate_path,key_path,enabled:true}
- Client masque / warp_masque: tls_server_name + insecure -> outbound_tls
- server_auth.mtls (auth-lab only): removed; CA PEM list moved to tls.client_certificate
  with tls.client_authentication = verify-if-given when mtls was present.
"""
from __future__ import annotations

import json
import sys
from pathlib import Path


def is_masque_family(ep: dict) -> bool:
    t = str(ep.get("type", "")).strip()
    return t in ("masque", "warp_masque")


def is_server_ep(ep: dict) -> bool:
    if str(ep.get("mode", "")).lower() == "server":
        return True
    return bool(ep.get("listen_port")) and ep.get("listen") is not None


def patch_server_auth_mtls(ep: dict) -> None:
    sa = ep.get("server_auth")
    if not isinstance(sa, dict):
        return
    mtls = sa.pop("mtls", None)
    if not isinstance(mtls, dict):
        return
    tls = ep.get("tls")
    if not isinstance(tls, dict):
        tls = {}
        ep["tls"] = tls
    ca = mtls.get("ca")
    if isinstance(ca, list) and ca:
        tls["client_certificate"] = ca
        tls["client_authentication"] = "verify-if-given"


def patch_endpoint(ep: dict) -> bool:
    if not isinstance(ep, dict) or not is_masque_family(ep):
        return False
    changed = False
    if is_server_ep(ep):
        cert = ep.pop("certificate", None)
        key = ep.pop("key", None)
        if cert is not None or key is not None:
            tls = ep.get("tls")
            if not isinstance(tls, dict):
                tls = {}
                ep["tls"] = tls
            tls["enabled"] = True
            if isinstance(cert, str) and cert.strip():
                tls["certificate_path"] = cert.strip()
            if isinstance(key, str) and key.strip():
                tls["key_path"] = key.strip()
            changed = True
        patch_server_auth_mtls(ep)
    else:
        sn = ep.pop("tls_server_name", None)
        ins = ep.pop("insecure", None)
        if sn is not None or ins is not None:
            ot: dict = {"enabled": True}
            if isinstance(sn, str) and sn.strip():
                ot["server_name"] = sn.strip()
            if ins is not None:
                ot["insecure"] = bool(ins)
            ep["outbound_tls"] = ot
            changed = True
    for k in ("tls_server_name", "insecure", "certificate", "key"):
        if k in ep:
            ep.pop(k, None)
            changed = True
    return changed


def patch_doc(doc: dict) -> bool:
    changed = False
    for key in ("endpoints",):
        arr = doc.get(key)
        if not isinstance(arr, list):
            continue
        for i, ep in enumerate(arr):
            if isinstance(ep, dict) and patch_endpoint(ep):
                arr[i] = ep
                changed = True
    return changed


def main() -> int:
    root = Path(__file__).resolve().parents[1] / "configs"
    if not root.is_dir():
        print("no configs dir", root, file=sys.stderr)
        return 1
    n = 0
    for path in sorted(root.glob("*.json")):
        if "masque" not in path.name.lower():
            continue
        try:
            text = path.read_text(encoding="utf-8")
            doc = json.loads(text)
        except Exception as e:
            print("skip", path, e, file=sys.stderr)
            continue
        if not isinstance(doc, dict):
            continue
        if not patch_doc(doc):
            continue
        path.write_text(json.dumps(doc, indent=2, ensure_ascii=False) + "\n", encoding="utf-8")
        n += 1
        print("patched", path.name)
    print("total", n)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
