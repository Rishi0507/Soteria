"""Marketplace listing sources.

* ``file``: JSON listing exports (or a mocked liquidation feed) in a folder.
  Format: a list of objects with listing_id, marketplace, url, title,
  description, seller, price, posted_at, image_text, barcode.
* ``shopify``: a discount / liquidation grocer that runs on Shopify exposes
  ``/products.json`` publicly — real listings, no key. Barcodes are not
  public, so matching relies on title/brand text and lot/date codes sellers
  put in descriptions ("short dated", "best by 06/2027", "lot ...").
* ``ebay``: eBay Browse API item summary search (free developer account;
  EBAY_CLIENT_ID / EBAY_CLIENT_SECRET, client-credentials OAuth). Optional.
"""

from __future__ import annotations

import base64
import json
import logging
import os
import re
import time
from dataclasses import dataclass

import requests

from .model import Listing

log = logging.getLogger("antievasion.sources")
UA = "Mozilla/5.0 (compatible; Soteria-AntiEvasion/0.1; +mailto:ops@soteria.dev)"
_tag = re.compile(r"<[^>]+>")


@dataclass
class SourceSpec:
    name: str
    kind: str  # file | shopify | ebay
    url: str = ""  # folder path, storefront URL, or (ebay) ignored
    query: str = ""  # ebay keyword query; shopify collection filter (substring of product_type/tags)


def _session() -> requests.Session:
    s = requests.Session()
    s.headers["User-Agent"] = UA
    return s


# ---- file ------------------------------------------------------------------

def load_listings(path: str, marketplace: str = "") -> list[Listing]:
    """Read one JSON listings file (a list, or {"listings": [...]})."""
    with open(path, encoding="utf-8") as f:
        rows = json.load(f)
    if isinstance(rows, dict):
        rows = rows.get("listings", [])
    out: list[Listing] = []
    for r in rows:
        out.append(Listing(
            listing_id=str(r.get("listing_id") or r.get("id") or r["url"]),
            marketplace=r.get("marketplace") or marketplace or os.path.splitext(os.path.basename(path))[0],
            url=r["url"], title=r.get("title", ""), description=r.get("description", ""),
            seller=r.get("seller", ""), price=str(r.get("price", "")), posted_at=r.get("posted_at", ""),
            image_text=r.get("image_text", ""), barcode=str(r.get("barcode", "")), raw=r,
        ))
    return out


def fetch_file(spec: SourceSpec) -> list[Listing]:
    if not os.path.isdir(spec.url):
        raise FileNotFoundError(f"{spec.name}: listing folder {spec.url!r} not found")
    out: list[Listing] = []
    for name in sorted(os.listdir(spec.url)):
        if name.endswith(".json"):
            out.extend(load_listings(os.path.join(spec.url, name), spec.name))
    return out


# ---- shopify storefront ------------------------------------------------------

def fetch_shopify(spec: SourceSpec, session: requests.Session | None = None) -> list[Listing]:
    session = session or _session()
    base = spec.url.rstrip("/")
    out: list[Listing] = []
    for page in range(1, 21):
        r = session.get(f"{base}/products.json", params={"limit": 250, "page": page}, timeout=20)
        if r.status_code != 200:
            raise RuntimeError(f"{spec.name}: HTTP {r.status_code}")
        products = r.json().get("products", [])
        for p in products:
            hay = f"{p.get('product_type', '')} {' '.join(p.get('tags') or [])}".lower()
            if spec.query and spec.query.lower() not in hay:
                continue
            desc = _tag.sub(" ", p.get("body_html") or "")
            for v in p.get("variants") or []:
                title = p.get("title", "")
                if v.get("title") and v["title"] != "Default Title":
                    title += " - " + v["title"]
                out.append(Listing(
                    listing_id=f"{spec.name}:variant:{v.get('id')}", marketplace=spec.name,
                    url=f"{base}/products/{p.get('handle', '')}", title=title, description=desc[:2000],
                    seller=p.get("vendor", ""), price=str(v.get("price", "")), posted_at=p.get("published_at") or "",
                    barcode=str(v.get("barcode") or ""), raw={"product_id": p.get("id"), "variant_id": v.get("id"), "sku": v.get("sku")},
                ))
        if len(products) < 250:
            break
        time.sleep(0.5)
    return out


# ---- eBay Browse API ---------------------------------------------------------------

_ebay_token: dict = {}


def _ebay_token_get(session: requests.Session) -> str:
    cid, secret = os.environ.get("EBAY_CLIENT_ID", ""), os.environ.get("EBAY_CLIENT_SECRET", "")
    if not cid or not secret:
        raise RuntimeError("ebay: EBAY_CLIENT_ID / EBAY_CLIENT_SECRET not set")
    if _ebay_token.get("exp", 0) > time.time() + 60:
        return _ebay_token["tok"]
    r = session.post(
        "https://api.ebay.com/identity/v1/oauth2/token",
        headers={"Authorization": "Basic " + base64.b64encode(f"{cid}:{secret}".encode()).decode(), "Content-Type": "application/x-www-form-urlencoded"},
        data={"grant_type": "client_credentials", "scope": "https://api.ebay.com/oauth/api_scope"},
        timeout=20,
    )
    if r.status_code != 200:
        raise RuntimeError(f"ebay token: HTTP {r.status_code}: {r.text[:200]}")
    j = r.json()
    _ebay_token.update(tok=j["access_token"], exp=time.time() + int(j.get("expires_in", 7200)))
    return _ebay_token["tok"]


def fetch_ebay(spec: SourceSpec, session: requests.Session | None = None, limit: int = 50) -> list[Listing]:
    session = session or _session()
    tok = _ebay_token_get(session)
    r = session.get(
        "https://api.ebay.com/buy/browse/v1/item_summary/search",
        params={"q": spec.query, "limit": limit, "category_ids": "14308"},  # Food & Beverages
        headers={"Authorization": f"Bearer {tok}", "X-EBAY-C-MARKETPLACE-ID": "EBAY_US"},
        timeout=30,
    )
    if r.status_code != 200:
        raise RuntimeError(f"ebay search: HTTP {r.status_code}: {r.text[:200]}")
    out = []
    for it in r.json().get("itemSummaries", []):
        out.append(Listing(
            listing_id=f"ebay:{it.get('itemId')}", marketplace="ebay", url=it.get("itemWebUrl", ""), title=it.get("title", ""),
            description=(it.get("shortDescription") or ""), seller=(it.get("seller") or {}).get("username", ""),
            price=str((it.get("price") or {}).get("value", "")), posted_at=it.get("itemCreationDate", "") or "",
            barcode="", raw=it,
        ))
    return out


def fetch(spec: SourceSpec) -> list[Listing]:
    if spec.kind == "file":
        return fetch_file(spec)
    if spec.kind == "shopify":
        return fetch_shopify(spec)
    if spec.kind == "ebay":
        return fetch_ebay(spec)
    raise ValueError(f"{spec.name}: unknown source kind {spec.kind!r}")
