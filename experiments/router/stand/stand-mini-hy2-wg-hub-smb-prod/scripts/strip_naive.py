"""Remove all naive inbounds/outbounds and references (sing-box does not support tls.insecure on naive)."""
from __future__ import annotations

import json
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]

NAIVE_INBOUND_TAGS = frozenset(
    {"naive-in", "naive-quic-in", "naive-quic-wg-in", "naive-wg-in"}
)


def strip_client(data: dict) -> None:
    out = data["outbounds"]
    naive_tags = {o["tag"] for o in out if o.get("type") == "naive"}
    data["outbounds"] = [o for o in out if o.get("type") != "naive"]
    for o in data["outbounds"]:
        if o.get("type") in ("selector", "urltest") and isinstance(o.get("outbounds"), list):
            o["outbounds"] = [t for t in o["outbounds"] if t not in naive_tags]
    if naive_tags:
        print("removed naive outbounds:", sorted(naive_tags))


def strip_server(data: dict) -> None:
    before = len(data["inbounds"])
    data["inbounds"] = [i for i in data["inbounds"] if i.get("type") != "naive"]
    print("server inbounds:", before, "->", len(data["inbounds"]))

    new_rules: list[dict] = []
    for r in data["route"]["rules"]:
        ib = r.get("inbound")
        if not isinstance(ib, list):
            new_rules.append(r)
            continue
        if ib == ["naive-wg-in"] or ib == ["naive-quic-wg-in"]:
            continue
        if any(t in NAIVE_INBOUND_TAGS for t in ib):
            ib2 = [t for t in ib if t not in NAIVE_INBOUND_TAGS]
            if not ib2:
                continue
            r = dict(r)
            r["inbound"] = ib2
        new_rules.append(r)
    data["route"]["rules"] = new_rules


def main() -> None:
    srv = ROOT / "server" / "compose" / "config.json"
    d = json.loads(srv.read_text(encoding="utf-8"))
    strip_server(d)
    srv.write_text(json.dumps(d, indent=2, ensure_ascii=False) + "\n", encoding="utf-8")
    print("wrote", srv)

    for n in range(1, 5):
        p = ROOT / f"client{n}" / "config.json"
        d = json.loads(p.read_text(encoding="utf-8"))
        strip_client(d)
        p.write_text(json.dumps(d, indent=2, ensure_ascii=False) + "\n", encoding="utf-8")
        print("wrote", p)


if __name__ == "__main__":
    main()
