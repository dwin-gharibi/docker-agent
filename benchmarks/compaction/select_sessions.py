#!/usr/bin/env python3
"""Select a stratified sample of sessions for the compaction benchmark.

Buckets root sessions by content size (S/M/L/XL) and tool-call density
(none/few/many), then picks an evenly spread sample from each bucket so the
benchmark covers short chat-only sessions as well as long tool-heavy ones.

Usage: select_sessions.py <session.db> [count]  (writes selected.json)
"""
import json
import sqlite3
import sys
from collections import defaultdict

# Target sample distribution; scaled proportionally to the requested count.
TARGETS = {
    ("S", "none"): 6, ("S", "few"): 8,
    ("M", "none"): 3, ("M", "few"): 7, ("M", "many"): 6,
    ("L", "few"): 4, ("L", "many"): 8,
    ("XL", "many"): 8,
}

STATS_SQL = """
SELECT s.id, s.title,
  COUNT(i.id) AS n_items,
  SUM(LENGTH(COALESCE(i.message_json,''))) AS bytes,
  SUM(CASE WHEN i.message_json LIKE '%"tool_calls":%' THEN 1 ELSE 0 END) AS n_toolcall_msgs
FROM sessions s JOIN session_items i ON i.session_id = s.id
WHERE s.parent_id IS NULL AND i.item_type='message'
GROUP BY s.id
HAVING bytes >= 2000 AND n_items >= 4
"""


def size_bucket(b):
    return "S" if b < 20_000 else "M" if b < 100_000 else "L" if b < 400_000 else "XL"


def tools_bucket(n):
    return "none" if n == 0 else "few" if n < 10 else "many"


def spread(items, n):
    """Pick n items evenly spread across the (deterministically ordered) list."""
    if len(items) <= n:
        return items
    step = len(items) / n
    return [items[int(i * step)] for i in range(n)]


def main():
    if len(sys.argv) < 2:
        sys.exit(__doc__)
    db, count = sys.argv[1], int(sys.argv[2]) if len(sys.argv) > 2 else 50

    buckets = defaultdict(list)
    for sid, title, n_items, nbytes, n_tools in sqlite3.connect(db).execute(STATS_SQL):
        buckets[(size_bucket(nbytes), tools_bucket(n_tools))].append({
            "id": sid, "title": title or "", "n_items": n_items,
            "bytes": nbytes, "n_toolcall_msgs": n_tools,
        })

    scale = count / sum(TARGETS.values())
    selected = []
    for key, target in TARGETS.items():
        items = sorted(buckets.get(key, []), key=lambda x: x["id"])
        n = max(1, round(target * scale)) if items else 0
        for item in spread(items, n):
            item["size"], item["tools"] = key
            selected.append(item)

    selected.sort(key=lambda x: x["bytes"])
    with open("selected.json", "w") as f:
        json.dump(selected, f, indent=1)
    print(f"selected {len(selected)} sessions -> selected.json")
    for key in TARGETS:
        n = sum(1 for s in selected if (s["size"], s["tools"]) == key)
        print(f"  {key[0]:>2}/{key[1]:<5} {n}")


if __name__ == "__main__":
    main()
