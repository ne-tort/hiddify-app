#!/usr/bin/env bash
# Включает kernel-маршруты для route_address (узкий список в JSON). Только для стенда.
set -euo pipefail
export PATCH_FILE="${1:-/root/warp-masque-stand/configs/warp-masque-live.server.local.json}"
python3 - <<'PY'
import pathlib, re, sys, os
p = pathlib.Path(os.environ["PATCH_FILE"])
t = p.read_text()
t2 = re.sub(r'"auto_route"\s*:\s*false', '"auto_route": true', t, count=1)
if t == t2:
    print("no change (already true or pattern missing)", file=sys.stderr)
    sys.exit(1)
p.write_text(t2)
print("patched:", p)
PY
