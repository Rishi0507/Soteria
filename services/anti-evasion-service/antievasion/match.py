"""Deterministic pre-filter: which listings could be a recalled lot?

The LLM judge is expensive and fallible, so it only sees candidates that a
cheap, explainable pass already finds plausible. Scoring:

    GTIN printed in the listing (barcode field or text)    +0.60  near-certain product identity
    a recalled lot code appears verbatim in the listing     +0.35  the smoking gun
    a recalled best-by / expiry date appears in the listing +0.25  the date code that shipped with the lot
    brand present                                           +0.15
    title similarity (token_set_ratio / 100) * 0.30          product-line resemblance
    listing posted BEFORE the recall date                   -0.50  not arbitrage, just old stock listed early

Candidates below MIN_SCORE are dropped without a model call.
"""

from __future__ import annotations

import re

from rapidfuzz import fuzz

from .model import Candidate, Listing, Recall, gtin14, parse_ts

MIN_SCORE = 0.35
_ws = re.compile(r"\s+")


def _norm(s: str) -> str:
    return _ws.sub(" ", s.lower()).strip()


def _digit_runs(text: str) -> set[str]:
    out = set()
    for m in re.findall(r"(?:\d[ -]?){11,13}\d", text):
        g = gtin14(m)
        if g:
            out.add(g)
    return out


def score(recall: Recall, listing: Listing) -> Candidate:
    text = listing.text
    tnorm = _norm(text)
    s, signals = 0.0, []

    gtin_hit = False
    if recall.gtin:
        codes = _digit_runs(text) | ({gtin14(listing.barcode)} if listing.barcode else set())
        if recall.gtin in codes:
            gtin_hit = True
            s += 0.60
            signals.append(f"gtin {recall.gtin.lstrip('0')} present")

    lot_hit = ""
    for lot in recall.lot_codes:
        if lot and _norm(lot) in tnorm:
            lot_hit = lot
            s += 0.35
            signals.append(f"lot {lot} present")
            break

    date_hit = ""
    if recall.date_codes:
        keys = date_keys(text)
        for dc in recall.date_codes:
            k = date_key(dc)
            if k and k in keys:
                date_hit = dc
                s += 0.25
                signals.append(f"date code {dc} present")
                break

    if recall.brand and _norm(recall.brand) in tnorm:
        s += 0.15
        signals.append("brand present")

    if recall.product_title:
        sim = fuzz.token_set_ratio(_norm(recall.product_title), _norm(listing.title)) / 100.0
        s += 0.30 * sim
        if sim >= 0.6:
            signals.append(f"title similarity {sim:.2f}")

    posted, recalled = parse_ts(listing.posted_at), parse_ts(recall.recalled_at)
    if posted and recalled and posted < recalled:
        s -= 0.50
        signals.append(f"posted {posted.date()} before recall {recalled.date()}")

    return Candidate(recall=recall, listing=listing, score=max(0.0, min(1.0, round(s, 2))), signals=signals, gtin_hit=gtin_hit, lot_hit=lot_hit, date_hit=date_hit)


_MONTHS = {m: i for i, m in enumerate(["jan", "feb", "mar", "apr", "may", "jun", "jul", "aug", "sep", "oct", "nov", "dec"], 1)}
_pat_mdy = re.compile(r"\b([a-z]{3})[a-z]*\.?\s+(\d{1,2}),?\s+(\d{4})\b")  # January 28, 2027
_pat_dmy = re.compile(r"\b(\d{1,2})\s+([a-z]{3})[a-z]*\.?\s+(\d{4})\b")  # 31 JUL 2027
_pat_us = re.compile(r"\b(\d{1,2})/(\d{1,2})/(\d{2,4})\b")  # 1/28/2027, 01/28/27
_pat_iso = re.compile(r"\b(\d{4})-(\d{2})-(\d{2})\b")  # 2027-01-28


def date_keys(text: str) -> set[str]:
    """Every date in the text normalised to YYYY-MM-DD."""
    t = text.lower()
    out: set[str] = set()
    for m in _pat_mdy.finditer(t):
        if mo := _MONTHS.get(m.group(1)):
            out.add(f"{m.group(3)}-{mo:02d}-{int(m.group(2)):02d}")
    for m in _pat_dmy.finditer(t):
        if mo := _MONTHS.get(m.group(2)):
            out.add(f"{m.group(3)}-{mo:02d}-{int(m.group(1)):02d}")
    for m in _pat_us.finditer(t):
        y = m.group(3)
        y = "20" + y if len(y) == 2 else y
        out.add(f"{y}-{int(m.group(1)):02d}-{int(m.group(2)):02d}")
    for m in _pat_iso.finditer(t):
        out.add(f"{m.group(1)}-{m.group(2)}-{m.group(3)}")
    return out


def date_key(s: str) -> str:
    ks = date_keys(s)
    return sorted(ks)[0] if ks else ""


def candidates(recalls: list[Recall], listings: list[Listing], min_score: float = MIN_SCORE) -> list[Candidate]:
    out = [score(r, l) for r in recalls for l in listings]
    out = [c for c in out if c.score >= min_score]
    out.sort(key=lambda c: -c.score)
    return out
