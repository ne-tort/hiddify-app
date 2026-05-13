#!/usr/bin/env bash
# Добавить явные template_* с публичным host:port (должны совпадать с :authority клиента).
# Иначе при listen :: дефолтный authority шаблонов = 127.0.0.1 → masque tcp connect-stream failed.
set -eu
export MASQUE_PUBLIC_IP="${MASQUE_PUBLIC_IP:-163.5.180.181}"
export MASQUE_PORT="${MASQUE_PORT:-18443}"
export CFG="${MASQUE_STAND_CONFIG:-/etc/sing-box/masque-stand/sing-box.json}"

python3 - <<'PY'
import json, os
ip = os.environ["MASQUE_PUBLIC_IP"]
port = os.environ["MASQUE_PORT"]
cfg = os.environ["CFG"]
with open(cfg) as f:
    c = json.load(f)
ep = c["endpoints"][0]
ep["template_udp"] = f"https://{ip}:{port}/masque/udp/{{target_host}}/{{target_port}}"
ep["template_ip"] = f"https://{ip}:{port}/masque/ip"
ep["template_tcp"] = f"https://{ip}:{port}/masque/tcp/{{target_host}}/{{target_port}}"
with open(cfg, "w") as f:
    json.dump(c, f, indent=2)
print("patched", cfg)
PY

/usr/local/bin/sing-box check -c "$CFG"
systemctl restart sing-box-masque-stand.service
sleep 1
systemctl is-active sing-box-masque-stand.service
