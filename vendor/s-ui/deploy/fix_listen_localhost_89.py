#!/usr/bin/env python3
"""Cascade VPS: nginx терминатирует TLS на 0.0.0.0:2095, backend s-ui — 127.0.0.1:12095
(см. itvzlet2.ai-qwerty.ru.conf: proxy_pass https://127.0.0.1:12095/itvzlet/)."""
import sqlite3
import sys

DB = "/opt/hiddify-app/vendor/s-ui/db/s-ui.db"


def main() -> None:
    conn = sqlite3.connect(DB)
    cur = conn.cursor()

    def upsert(key: str, value: str) -> None:
        row = cur.execute("SELECT id FROM settings WHERE key = ?", (key,)).fetchone()
        if row:
            cur.execute("UPDATE settings SET value = ? WHERE key = ?", (value, key))
        else:
            cur.execute("INSERT INTO settings (key, value) VALUES (?, ?)", (key, value))

    upsert("webListen", "127.0.0.1")
    upsert("subListen", "127.0.0.1")
    upsert("webPort", "12095")
    upsert("subPort", "12096")
    conn.commit()
    for k in ("webListen", "subListen", "webPort", "subPort"):
        r = cur.execute("SELECT key, value FROM settings WHERE key = ?", (k,)).fetchone()
        print(r)
    conn.close()
    print("OK: web 127.0.0.1:12095, sub 127.0.0.1:12096", file=sys.stderr)


if __name__ == "__main__":
    main()
