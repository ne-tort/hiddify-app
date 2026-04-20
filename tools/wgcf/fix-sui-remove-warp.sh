#!/usr/bin/env bash
set -euo pipefail
DB="/opt/hiddify-app/vendor/s-ui/db/s-ui.db"
docker stop s-ui-local
cp "$DB" "${DB}.bak-$(date +%Y%m%d%H%M%S)"
python3 <<'PY'
import sqlite3
db = "/opt/hiddify-app/vendor/s-ui/db/s-ui.db"
c = sqlite3.connect(db)
c.execute("DELETE FROM endpoints WHERE tag='warp-exit'")
c.commit()
c.close()
PY
docker start s-ui-local
sleep 5
docker ps --filter name=s-ui-local
docker logs --tail 25 s-ui-local 2>&1
