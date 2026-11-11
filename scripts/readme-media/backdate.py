"""Spread a seeded instance's runs over the past week (one nightly pair per day) and date the
configuration before them, so screenshots show realistic history. Run with the instance stopped.

Usage: python3 backdate.py <path-to-airval.db>
"""
import sqlite3
import sys
from datetime import datetime, timedelta, timezone

db = sqlite3.connect(sys.argv[1])
fmt = lambda d: d.strftime("%Y-%m-%d %H:%M:%S.%f") + "+00:00"
parse = lambda s: datetime.fromisoformat(s) if s else None
now = datetime.now(timezone.utc).replace(minute=0, second=0, microsecond=0)

rows = db.execute("select id, created_at, started_at, finished_at from runs order by created_at").fetchall()
pairs = len(rows) // 2
for i, (rid, c, s, f) in enumerate(rows):
    days_ago = (pairs - 1) - i // 2
    day = now - timedelta(days=days_ago)
    base = day - timedelta(hours=3) if days_ago == 0 else day.replace(hour=2)
    when = base + timedelta(minutes=4 * (i % 2) + 7 * (i // 2))
    shift = when - parse(c)
    db.execute("update runs set created_at=?, started_at=?, finished_at=? where id=?",
               (fmt(parse(c) + shift), fmt(parse(s) + shift) if s else None, fmt(parse(f) + shift) if f else None, rid))

config_time = fmt(now - timedelta(days=pairs + 1))
skip = {"runs", "results", "scores", "run_aggregates", "run_shards", "goose_db_version", "sessions", "cache_entries", "audit_log"}
for (table,) in db.execute("select name from sqlite_master where type='table'").fetchall():
    if table in skip:
        continue
    cols = {c[1] for c in db.execute(f"pragma table_info({table})")}
    sets = [f"{c}=?" for c in ("created_at", "updated_at") if c in cols]
    if sets:
        db.execute(f"update {table} set {', '.join(sets)}", [config_time] * len(sets))
db.commit()
