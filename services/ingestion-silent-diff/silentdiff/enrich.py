"""Optional UPC enrichment via Open Food Facts.

Public Shopify catalogs do not expose barcodes, and the resolution-service
matches best on UPC. For the handful of SKUs that actually vanish we can
afford one OFF search each (the search endpoint allows ~10 req/min; a cache
keeps repeats free). Enrichment is best-effort: no match → no ``upc``.
"""

from __future__ import annotations

import logging
import time

import requests
from rapidfuzz import fuzz

from .sources import USER_AGENT

log = logging.getLogger("silentdiff.enrich")
SEARCH_URL = "https://world.openfoodfacts.org/cgi/search.pl"
MIN_INTERVAL = 6.5  # seconds between searches → under 10/min


class OFFEnricher:
    def __init__(self, session: requests.Session | None = None, min_score: int = 80):
        self.session = session or requests.Session()
        self.session.headers["User-Agent"] = USER_AGENT
        self.min_score = min_score
        self.cache: dict[str, str] = {}
        self._last = 0.0

    def upc_for(self, brand: str, title: str) -> str:
        q = f"{brand} {title}".strip()
        if not q:
            return ""
        if q in self.cache:
            return self.cache[q]
        wait = MIN_INTERVAL - (time.monotonic() - self._last)
        if wait > 0:
            time.sleep(wait)
        self._last = time.monotonic()
        upc = ""
        try:
            r = self.session.get(
                SEARCH_URL,
                params={"search_terms": q, "search_simple": 1, "action": "process", "json": 1, "page_size": 10, "fields": "code,product_name,brands"},
                timeout=20,
            )
            if r.status_code == 200:
                upc = self._best(r.json().get("products", []), brand, title)
            else:
                log.info("off search HTTP %s for %r", r.status_code, q)
        except (requests.RequestException, ValueError) as e:
            log.info("off search failed for %r: %s", q, e)
        self.cache[q] = upc
        return upc

    def _best(self, products: list[dict], brand: str, title: str) -> str:
        want = f"{brand} {title}".lower()
        best, best_score = "", 0
        for p in products:
            got = f"{p.get('brands', '')} {p.get('product_name', '')}".lower()
            score = fuzz.token_set_ratio(want, got)
            if score > best_score and p.get("code"):
                best, best_score = p["code"], score
        return best if best_score >= self.min_score else ""
