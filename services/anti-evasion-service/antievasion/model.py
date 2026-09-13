"""Domain types for recall-arbitrage detection.

A Recall is a product+lot the retailer has contained (from
containment.action.taken.v1) or a hand-seeded watch-list entry. A Listing
is something for sale on a marketplace. A Flag says a listing looks like a
recalled lot resurfacing after the recall date.
"""

from __future__ import annotations

import re
from dataclasses import dataclass, field
from datetime import datetime, timezone


def now_iso() -> str:
    return datetime.now(timezone.utc).isoformat(timespec="seconds").replace("+00:00", "Z")


def parse_ts(s: str | None) -> datetime | None:
    if not s:
        return None
    try:
        d = datetime.fromisoformat(s.replace("Z", "+00:00"))
        return d if d.tzinfo else d.replace(tzinfo=timezone.utc)
    except ValueError:
        return None


_non_digit = re.compile(r"\D")


def gtin14(code: str | None) -> str:
    """Zero-pad a 12/13/14-digit code to GTIN-14; '' if not GTIN-shaped."""
    d = _non_digit.sub("", code or "")
    return d.zfill(14) if len(d) in (12, 13, 14) else ""


@dataclass
class Recall:
    incident_id: str
    gtin: str = ""  # GTIN-14
    lot_codes: list[str] = field(default_factory=list)
    date_codes: list[str] = field(default_factory=list)  # best-by / expiry text tied to the recalled lots
    brand: str = ""
    product_title: str = ""
    hazard: str = ""
    recalled_at: str = ""  # ISO; listings posted before this are not evasion
    source: str = "watchlist"  # watchlist | containment

    @property
    def key(self) -> str:
        return f"{self.incident_id}|{self.gtin or self.product_title}"


@dataclass
class Listing:
    listing_id: str
    marketplace: str
    url: str
    title: str
    description: str = ""
    seller: str = ""
    price: str = ""
    posted_at: str = ""  # ISO if known
    image_text: str = ""  # OCR / caption text from photos, if the source provides it
    barcode: str = ""  # explicit product code if the marketplace exposes one
    raw: dict = field(default_factory=dict)

    @property
    def text(self) -> str:
        return " ".join(x for x in (self.title, self.description, self.image_text, self.barcode) if x)


@dataclass
class Candidate:
    recall: Recall
    listing: Listing
    score: float  # deterministic pre-score 0..1
    signals: list[str]
    gtin_hit: bool = False
    lot_hit: str = ""  # the lot code found in the listing text, if any
    date_hit: str = ""  # the recalled best-by / expiry date found in the listing, if any


@dataclass
class Verdict:
    match: bool
    confidence: float
    reasons: list[str]
    lot_visible: bool = False
    model: str = "rules"


@dataclass
class Flag:
    incident_id: str
    flag_id: str
    marketplace: str
    listing_url: str
    listing_title: str
    seller: str
    gtin: str
    lot_code: str
    observed_at: str
    confidence: float
    evidence: list[str]

    def payload(self) -> dict:
        p = {
            "incident_id": self.incident_id,
            "flag_id": self.flag_id,
            "marketplace": self.marketplace,
            "listing_url": self.listing_url,
            "observed_at": self.observed_at,
            "confidence": round(self.confidence, 2),
        }
        if self.listing_title:
            p["listing_title"] = self.listing_title
        if self.seller:
            p["seller"] = self.seller
        if self.gtin:
            p["gtin"] = self.gtin
        if self.lot_code:
            p["lot_code"] = self.lot_code
        if self.evidence:
            p["evidence"] = self.evidence
        return p
