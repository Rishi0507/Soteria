"""Snapshot diffing and vanish-confidence scoring.

The output of a diff is a list of Vanished signals. Confidence encodes how
much a disappearance looks like a quiet withdrawal rather than housekeeping:

    base                                    0.50   variant missing from the new snapshot
    whole product gone (every variant)     +0.20   not just one size/flavour
    was in stock when it vanished          +0.15   out-of-stock items get delisted routinely
    vendor's other products still present  +0.10   the brand is still selling; this item specifically was pulled
    fuzzy title match to a NEW row >= 90   cap 0.35   renamed / re-packaged, not withdrawn
    fuzzy title match to a NEW row >= 75   -0.15      probably a rename
    seasonal / limited wording in title    -0.15   "holiday", "limited edition", "spooky" ...
    published < 14 days before vanishing   -0.10   short-lived test listing

clamped to [0.05, 0.99].

A snapshot that shrank by more than ``MAX_SHRINK`` (default 40%) is treated as
a fetch anomaly (site outage, pagination glitch, bot wall) and produces no
signals at all — better to miss one hour than to emit hundreds of false alarms.
"""

from __future__ import annotations

import re
from dataclasses import dataclass, field
from datetime import datetime, timedelta, timezone

from rapidfuzz import fuzz, process

from .sources import Row

MAX_SHRINK = 0.40
SEASONAL = re.compile(r"\b(holiday|limited[- ]edition|seasonal|spooky|halloween|christmas|valentine|easter|pumpkin spice|summer|winter)\b", re.I)


@dataclass
class Vanished:
    row: Row
    confidence: float
    reasons: list[str] = field(default_factory=list)
    renamed_to: str | None = None  # key of the new row it likely became
    last_seen_at: str = ""
    observed_at: str = ""


@dataclass
class DiffResult:
    source: str
    vanished: list[Vanished]
    appeared: list[Row]
    anomaly: str | None = None  # set when the diff was suppressed
    before_count: int = 0
    after_count: int = 0


def _label(r: Row) -> str:
    return " ".join(x for x in (r.title, r.variant_title) if x).strip().lower()


def _parse_ts(s: str) -> datetime | None:
    if not s:
        return None
    try:
        return datetime.fromisoformat(s.replace("Z", "+00:00"))
    except ValueError:
        return None


def diff(
    source: str,
    before: dict[str, Row],
    after: dict[str, Row],
    before_at: str,
    after_at: str,
    max_shrink: float = MAX_SHRINK,
) -> DiffResult:
    res = DiffResult(source=source, vanished=[], appeared=[], before_count=len(before), after_count=len(after))
    if not before:
        res.anomaly = "no previous snapshot"
        return res
    if not after:
        res.anomaly = "new snapshot is empty (fetch failure?)"
        return res
    shrink = 1 - len(after) / len(before)
    if shrink > max_shrink:
        res.anomaly = f"snapshot shrank {shrink:.0%} ({len(before)} → {len(after)}); suppressed as a fetch anomaly"
        return res

    gone_keys = [k for k in before if k not in after]
    new_keys = [k for k in after if k not in before]
    res.appeared = [after[k] for k in new_keys]

    # Which products vanished entirely?
    products_after = {r.product_key for r in after.values()}
    vendors_after = {r.vendor.lower() for r in after.values() if r.vendor}
    new_labels = {k: _label(after[k]) for k in new_keys}
    after_ts = _parse_ts(after_at) or datetime.now(timezone.utc)

    for k in gone_keys:
        r = before[k]
        conf = 0.50
        reasons = ["missing from new snapshot"]
        if r.product_key and r.product_key not in products_after:
            conf += 0.20
            reasons.append("whole product gone")
        if r.available is True:
            conf += 0.15
            reasons.append("was in stock")
        if r.vendor and r.vendor.lower() in vendors_after:
            conf += 0.10
            reasons.append("vendor still listing other products")

        renamed_to = None
        cap = 0.99
        if new_labels:
            match = process.extractOne(_label(r), new_labels, scorer=fuzz.token_set_ratio)
            if match:
                _, score, nk = match
                if score >= 90:
                    cap = 0.35
                    renamed_to = nk
                    reasons.append(f"near-identical new listing {nk} ({score:.0f}%): likely renamed")
                elif score >= 75:
                    conf -= 0.15
                    renamed_to = nk
                    reasons.append(f"similar new listing {nk} ({score:.0f}%)")
        if SEASONAL.search(_label(r)):
            conf -= 0.15
            reasons.append("seasonal/limited wording")
        pub = _parse_ts(r.published_at)
        if pub and after_ts - pub < timedelta(days=14):
            conf -= 0.10
            reasons.append("listed less than 14 days")

        conf = max(0.05, min(cap, round(conf, 2)))
        res.vanished.append(Vanished(row=r, confidence=conf, reasons=reasons, renamed_to=renamed_to, last_seen_at=before_at, observed_at=after_at))

    res.vanished.sort(key=lambda v: -v.confidence)
    return res
