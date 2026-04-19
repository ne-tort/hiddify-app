"""Generate client2..client4 from client1 (same structure, per-table secrets)."""
from __future__ import annotations

import json
from copy import deepcopy
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]

OLD_PW = "pass-client1"
OLD_USER = "client1"
OLD_TUIC = "11111111-1111-1111-1111-111111111111"
OLD_VLESS = "aaaaaaa1-1111-4111-8111-111111111111"

SPEC: list[tuple[int, str, str, str, str, str, str, str]] = [
    (
        2,
        "10.0.0.3/24",
        "MH2UUK81tirv1LZs4L2XHryQkj58ElZE0yFJ5W7YV1U=",
        "tun0",
        "pass-client2",
        "client2",
        "22222222-2222-2222-2222-222222222222",
        "aaaaaaa2-2222-4222-8222-222222222222",
    ),
    (
        3,
        "10.0.0.4/24",
        "8GRtNNP+0l91YagadAVFNQvA44b+YPzXX/+72LTPtko=",
        "tun0",
        "pass-client3",
        "client3",
        "33333333-3333-3333-3333-333333333333",
        "aaaaaaa3-3333-4333-8333-333333333333",
    ),
    (
        4,
        "10.0.0.5/24",
        "aPTsiWEl1gGSwezFOooBMauS7rZi9qw3mJk1/1VMiWU=",
        "tun0",
        "pass-client4",
        "client4",
        "44444444-4444-4444-4444-444444444444",
        "aaaaaaa4-4444-4444-8444-444444444444",
    ),
]


def _walk(o: object, pw: str, uname: str, tuic: str, vless: str) -> None:
    if isinstance(o, dict):
        for k, v in o.items():
            if isinstance(v, (dict, list)):
                _walk(v, pw, uname, tuic, vless)
            elif v == OLD_PW:
                o[k] = pw
            elif v == OLD_USER:
                o[k] = uname
            elif v == OLD_TUIC:
                o[k] = tuic
            elif v == OLD_VLESS:
                o[k] = vless
    elif isinstance(o, list):
        for i in o:
            _walk(i, pw, uname, tuic, vless)


def main() -> None:
    c1 = json.loads((ROOT / "client1" / "config.json").read_text(encoding="utf-8"))
    for num, addr, priv, ifname, pw, uname, tuic, vless in SPEC:
        cfg = deepcopy(c1)
        cfg["endpoints"][0]["address"] = [addr]
        cfg["endpoints"][0]["private_key"] = priv
        cfg["inbounds"][0]["interface_name"] = ifname
        _walk(cfg, pw, uname, tuic, vless)
        d = ROOT / f"client{num}"
        d.mkdir(exist_ok=True)
        (d / "config.json").write_text(
            json.dumps(cfg, indent=2, ensure_ascii=False) + "\n", encoding="utf-8"
        )
        print("wrote", d / "config.json")


if __name__ == "__main__":
    main()
