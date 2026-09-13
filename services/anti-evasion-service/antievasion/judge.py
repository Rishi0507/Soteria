"""LLM judge: is this listing the recalled lot?

The pre-filter says a listing *could* be the recalled product. The judge
reads the recall record and the listing side by side and answers whether it
is the same product, whether a recalled lot/date code is visible, and how
sure it is — with reasons. Groq (openai/gpt-oss-120b), JSON mode, cached.

Guardrail (deterministic, applied after the model):

* A flag is raised only if (a) the GTIN was found in the listing, or
  (b) a recalled lot code was found verbatim in the listing, or
  (c) the judge says match with confidence >= 0.75 AND the pre-score >= 0.5.
  The model cannot flag a listing the rules found implausible.
* Final confidence = 0.5 * pre-score + 0.5 * judge confidence, +0.1 if a lot
  code is verbatim, capped at 0.99. A judge that says "no" caps it at 0.3.
* Evidence must be quotes from the listing text; anything else is dropped.
"""

from __future__ import annotations

import hashlib
import json
import logging
import os
import sqlite3
import time

import requests

from .model import Candidate, Verdict

log = logging.getLogger("antievasion.judge")

SYSTEM = """You are the listing judge of a food-recall anti-evasion system. Retailers pull recalled lots from
sale; bad actors buy them cheaply and resell them on liquidation and resale marketplaces where the recall
was never seen. You are shown ONE recalled product (with its UPC/GTIN, lot codes and recall date) and ONE
marketplace listing. Decide whether the listing is offering that recalled product, and whether a recalled
lot or date code is visible in the listing.

The question is whether the listing offers the RECALLED LOT, not merely the same product line.

Rules:
- Same brand + same product line + same size = same product. A different flavour, size, or a different
  product from the same brand is NOT a match.
- match=true when it is the same product AND either (a) a recalled lot code or its best-by/expiry date is
  visible in the listing, or (b) the listing shows no lot / date information at all (then confidence <= 0.6).
- match=false when the listing shows a DIFFERENT lot code or best-by date than the recalled ones, or says
  the stock is a newer / unaffected lot.
- A lot code counts as visible only if it appears in the listing text or photo text verbatim.
- A listing posted before the recall date is not evasion (say match=false unless the lot is visible).
- "evidence" must be short verbatim quotes from the LISTING (max 4).
- Output ONLY a JSON object:
  {"match": true|false, "lot_visible": true|false, "confidence": 0-1, "reasons": ["..."], "evidence": ["..."]}"""


def _prompt(c: Candidate) -> str:
    r, l = c.recall, c.listing
    return (
        "RECALLED PRODUCT\n"
        f"  brand: {r.brand}\n  product: {r.product_title}\n  gtin: {r.gtin.lstrip('0') if r.gtin else '(unknown)'}\n"
        f"  lot codes: {', '.join(r.lot_codes) or '(none listed)'}\n  lot best-by / expiry: {', '.join(r.date_codes) or '(unknown)'}\n"
        f"  hazard: {r.hazard}\n  recall date: {r.recalled_at or '(unknown)'}\n\n"
        "LISTING\n"
        f"  marketplace: {l.marketplace}\n  seller: {l.seller}\n  posted: {l.posted_at or '(unknown)'}\n  price: {l.price}\n"
        f"  title: {l.title}\n  description: {l.description}\n  photo text: {l.image_text}\n  barcode field: {l.barcode}\n\n"
        f"Pre-filter signals: {', '.join(c.signals) or 'none'}"
    )


class Judge:
    """Base: rules-only verdict (no model). Subclasses add the LLM."""

    name = "rules"

    def judge(self, c: Candidate) -> Verdict:
        if c.gtin_hit or c.lot_hit:
            return Verdict(match=True, confidence=c.score, reasons=c.signals, lot_visible=bool(c.lot_hit), model=self.name)
        return Verdict(match=False, confidence=c.score, reasons=c.signals, model=self.name)


class GroqJudge(Judge):
    def __init__(self, api_key: str, model: str = "openai/gpt-oss-120b", cache_path: str = ":memory:"):
        self.api_key, self.model = api_key, model
        self.name = f"groq/{model}"
        self.session = requests.Session()
        self.db = sqlite3.connect(cache_path)
        self.db.execute("CREATE TABLE IF NOT EXISTS cache (key TEXT PRIMARY KEY, response TEXT NOT NULL)")

    def _cached(self, key: str) -> dict | None:
        row = self.db.execute("SELECT response FROM cache WHERE key = ?", (key,)).fetchone()
        return json.loads(row[0]) if row else None

    def _store(self, key: str, resp: dict) -> None:
        self.db.execute("INSERT OR REPLACE INTO cache (key, response) VALUES (?, ?)", (key, json.dumps(resp)))
        self.db.commit()

    def ask(self, prompt: str) -> dict:
        key = hashlib.sha256((self.model + "\0" + prompt).encode()).hexdigest()
        if (hit := self._cached(key)) is not None:
            return hit
        body = {
            "model": self.model,
            "temperature": 0,
            "max_tokens": 1024,
            "response_format": {"type": "json_object"},
            "messages": [{"role": "system", "content": SYSTEM}, {"role": "user", "content": prompt}],
        }
        last = None
        for attempt in range(4):
            try:
                r = self.session.post("https://api.groq.com/openai/v1/chat/completions", json=body, headers={"Authorization": f"Bearer {self.api_key}"}, timeout=60)
            except requests.RequestException as e:
                last = e
                time.sleep(2**attempt)
                continue
            if r.status_code == 429 or r.status_code >= 500:
                last = RuntimeError(f"groq HTTP {r.status_code}: {r.text[:200]}")
                time.sleep(float(r.headers.get("Retry-After", 2**attempt)))
                continue
            if r.status_code != 200:
                raise RuntimeError(f"groq HTTP {r.status_code}: {r.text[:300]}")
            content = r.json()["choices"][0]["message"]["content"]
            resp = json.loads(content)
            self._store(key, resp)
            return resp
        raise RuntimeError(f"groq: giving up: {last}")

    def judge(self, c: Candidate) -> Verdict:
        try:
            resp = self.ask(_prompt(c))
        except (RuntimeError, ValueError, KeyError) as e:
            log.warning("judge failed, falling back to rules: %s", e)
            return super().judge(c)
        listing_text = c.listing.text.lower()
        evidence = [q for q in resp.get("evidence", []) if isinstance(q, str) and q.strip() and q.strip().lower() in listing_text]
        reasons = [str(x) for x in resp.get("reasons", [])][:6]
        return Verdict(
            match=bool(resp.get("match")),
            confidence=max(0.0, min(1.0, float(resp.get("confidence", 0)))),
            reasons=reasons + [f"evidence: {q}" for q in evidence],
            lot_visible=bool(resp.get("lot_visible")) and bool(c.lot_hit),  # visible only if the rules also saw it
            model=self.name,
        )


def decide(c: Candidate, v: Verdict, min_pre_for_model_only: float = 0.5, min_judge_conf: float = 0.75) -> tuple[bool, float]:
    """Apply the guardrail; returns (flag?, final confidence).

    Hard evidence (GTIN or lot code verbatim in the listing) flags regardless
    of the judge, unless the listing predates the recall — old stock listed
    before the notice is not laundering — and then only a visible lot code
    keeps it. Soft evidence (product resemblance) flags only when the judge is
    confident AND the pre-score was already plausible.
    """
    predates = any(s.startswith("posted ") and "before recall" in s for s in c.signals)
    hard = c.gtin_hit or bool(c.lot_hit)
    # A matching best-by date is strong but not unique to the lot; it needs the judge's agreement.
    soft = v.match and v.confidence >= min_judge_conf and (c.score >= min_pre_for_model_only or bool(c.date_hit))
    if predates and not c.lot_hit:
        return False, min(c.score, 0.3)
    if not (hard or soft):
        return False, min(c.score, 0.3)
    conf = 0.5 * c.score + 0.5 * v.confidence
    if c.lot_hit:
        conf += 0.1
    if hard and not v.match:
        conf = min(conf, 0.6)  # identity is certain, the judge doubts the offer; surface it, but lower
    return True, round(max(0.05, min(conf, 0.99)), 2)
