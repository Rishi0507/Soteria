"""Catalog sources for silent-recall detection.

A source returns the current catalog of one manufacturer/distributor as a list
of Row dicts keyed by a stable ``key``. Two snapshots of the same source are
diffed to find SKUs that vanished without a public notice.

Real sources, no keys:

* ``shopify``: any public Shopify storefront exposes ``/products.json`` with
  products, variants, SKUs, vendor, availability and timestamps. Thousands of
  food brands sell direct on Shopify. Barcodes are NOT exposed publicly.
* ``sitemap``: ``sitemap.xml`` product URLs for brands not on Shopify.
  Coarser (one row per product URL), still a real removal signal.
"""

from __future__ import annotations

import re
import time
import xml.etree.ElementTree as ET
from dataclasses import dataclass, field
from typing import Iterable

import requests

USER_AGENT = "Mozilla/5.0 (compatible; Soteria-SilentDiff/0.1; +mailto:ops@soteria.dev)"
TIMEOUT = 20


class FetchError(RuntimeError):
    """The source could not be read (network, non-2xx, non-JSON, blocked)."""


@dataclass
class Row:
    key: str  # stable identity within the source, e.g. "variant:123"
    source: str
    kind: str  # "variant" | "product" | "url"
    sku: str = ""
    title: str = ""  # product title
    variant_title: str = ""
    vendor: str = ""
    product_type: str = ""
    handle: str = ""
    url: str = ""
    available: bool | None = None
    price: str = ""
    published_at: str = ""
    updated_at: str = ""
    product_key: str = ""  # groups variants of one product
    extra: dict = field(default_factory=dict)

    def to_dict(self) -> dict:
        d = self.__dict__.copy()
        return d

    @classmethod
    def from_dict(cls, d: dict) -> "Row":
        return cls(**{k: v for k, v in d.items() if k in cls.__dataclass_fields__})


@dataclass
class Source:
    name: str  # catalog_source in the event, e.g. "shopify:lesserevil.com"
    kind: str  # shopify | sitemap
    url: str  # storefront base URL or sitemap URL
    brand: str = ""  # brand_name override for the event


def _session() -> requests.Session:
    s = requests.Session()
    s.headers["User-Agent"] = USER_AGENT
    s.headers["Accept"] = "application/json, application/xml, text/xml;q=0.9, */*;q=0.5"
    return s


def _get(session: requests.Session, url: str, retries: int = 3) -> requests.Response:
    last: Exception | None = None
    for attempt in range(retries):
        try:
            r = session.get(url, timeout=TIMEOUT)
            if r.status_code == 429 or r.status_code >= 500:
                last = FetchError(f"HTTP {r.status_code} from {url}")
                time.sleep(min(2**attempt, 8))
                continue
            if r.status_code != 200:
                raise FetchError(f"HTTP {r.status_code} from {url}")
            return r
        except requests.RequestException as e:  # network
            last = e
            time.sleep(min(2**attempt, 8))
    raise FetchError(f"giving up on {url}: {last}")


# ---------------------------------------------------------------- shopify

def fetch_shopify(src: Source, session: requests.Session | None = None, page_size: int = 250, max_pages: int = 40) -> list[Row]:
    """Page through ``/products.json``. Shopify caps ``limit`` at 250."""
    session = session or _session()
    base = src.url.rstrip("/")
    rows: list[Row] = []
    for page in range(1, max_pages + 1):
        r = _get(session, f"{base}/products.json?limit={page_size}&page={page}")
        try:
            data = r.json()
        except ValueError as e:
            raise FetchError(f"{src.name}: products.json is not JSON ({e}); password-protected or not a Shopify store?")
        products = data.get("products")
        if products is None:
            raise FetchError(f"{src.name}: no 'products' key in response")
        rows.extend(parse_shopify_products(src, products))
        if len(products) < page_size:
            break
        time.sleep(0.5)  # be polite; storefront API is unauthenticated
    return rows


def parse_shopify_products(src: Source, products: Iterable[dict]) -> list[Row]:
    rows: list[Row] = []
    for p in products:
        pkey = f"product:{p.get('id')}"
        handle = p.get("handle", "")
        url = f"{src.url.rstrip('/')}/products/{handle}" if handle else ""
        for v in p.get("variants", []) or []:
            rows.append(
                Row(
                    key=f"variant:{v.get('id')}",
                    source=src.name,
                    kind="variant",
                    sku=(v.get("sku") or "").strip(),
                    title=(p.get("title") or "").strip(),
                    variant_title=(v.get("title") or "").strip() if (v.get("title") or "") != "Default Title" else "",
                    vendor=(p.get("vendor") or "").strip(),
                    product_type=(p.get("product_type") or "").strip(),
                    handle=handle,
                    url=url,
                    available=v.get("available"),
                    price=str(v.get("price") or ""),
                    published_at=p.get("published_at") or "",
                    updated_at=v.get("updated_at") or p.get("updated_at") or "",
                    product_key=pkey,
                    extra={"grams": v.get("grams"), "tags": p.get("tags")},
                )
            )
    return rows


# ---------------------------------------------------------------- sitemap

_LOC = re.compile(r"<loc>\s*([^<\s]+)\s*</loc>")


def fetch_sitemap(src: Source, session: requests.Session | None = None, max_children: int = 20) -> list[Row]:
    """Collect product URLs from a sitemap (following one level of sitemap index)."""
    session = session or _session()
    r = _get(session, src.url)
    locs = _locs(r.text)
    if not locs:
        raise FetchError(f"{src.name}: sitemap has no <loc> entries")
    urls: list[str] = []
    children = [u for u in locs if u.endswith(".xml")]
    if children:
        for child in children[:max_children]:
            if "product" in child.lower() or len(children) == 1:
                urls.extend(_locs(_get(session, child).text))
    else:
        urls = locs
    rows: list[Row] = []
    for u in urls:
        if "/products/" not in u and "/product/" not in u:
            continue
        slug = u.rstrip("/").rsplit("/", 1)[-1]
        rows.append(Row(key=f"url:{u}", source=src.name, kind="url", title=slug.replace("-", " "), handle=slug, url=u, vendor=src.brand, product_key=f"url:{u}"))
    return rows


def _locs(xml_text: str) -> list[str]:
    try:
        root = ET.fromstring(xml_text)
        return [el.text.strip() for el in root.iter() if el.tag.endswith("loc") and el.text]
    except ET.ParseError:
        return _LOC.findall(xml_text)


def fetch(src: Source, session: requests.Session | None = None) -> list[Row]:
    if src.kind == "shopify":
        return fetch_shopify(src, session)
    if src.kind == "sitemap":
        return fetch_sitemap(src, session)
    raise FetchError(f"{src.name}: unknown source kind {src.kind!r}")
