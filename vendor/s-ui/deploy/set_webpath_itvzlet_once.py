#!/usr/bin/env python3
"""Однократно: webPath = /itvzlet/ в SQLite s-ui (публичный URL совпадает с location в nginx)."""
import sqlite3
import sys

DB = "/opt/hiddify-app/vendor/s-ui/db/s-ui.db"
NEW = "/itvzlet/"


def main() -> None:
    con = sqlite3.connect(DB)
    cur = con.cursor()
    cur.execute("UPDATE settings SET value = ? WHERE key = 'webPath'", (NEW,))
    if cur.rowcount == 0:
        cur.execute("INSERT INTO settings (key, value) VALUES ('webPath', ?)", (NEW,))
    con.commit()
    print("OK:", cur.execute("SELECT key, value FROM settings WHERE key = 'webPath'").fetchone())
    con.close()


if __name__ == "__main__":
    main()
