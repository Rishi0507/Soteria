"""catalog.sku.vanished.v1 events and publishers (RabbitMQ / stdout)."""

from __future__ import annotations

import json
import logging
import sys
import uuid
from datetime import datetime, timezone

import pika
import pika.exceptions

from .diff import Vanished

EXCHANGE = "ingestion.x"
EVENT_TYPE = "ingestion.catalog.sku.vanished.v1"
PRODUCER = "ingestion-silent-diff"

log = logging.getLogger("silentdiff.events")


def _now() -> str:
    return datetime.now(timezone.utc).isoformat(timespec="milliseconds").replace("+00:00", "Z")


def payload(v: Vanished, upc: str = "", brand: str = "") -> dict:
    """The contract payload (contracts/events/catalog.sku.vanished.v1.json)."""
    r = v.row
    desc = " ".join(x for x in (r.title, r.variant_title) if x)
    if r.url:
        desc = f"{desc} ({r.url})"
    p = {
        "catalog_source": r.source,
        # SKU is required by the contract; fall back to the source key so a
        # SKU-less listing (sitemap URL) is still a usable signal.
        "sku": r.sku or r.key,
        "product_description": desc,
        "brand_name": brand or r.vendor,
        "observed_at": v.observed_at or _now(),
        "vanish_confidence": v.confidence,
    }
    if upc:
        p["upc"] = upc
    if v.last_seen_at:
        p["last_seen_at"] = v.last_seen_at
    return p


def envelope(p: dict) -> dict:
    """_envelope.v1.json wrapper; correlation is left to the resolver, which
    derives the incident from (source, sku)."""
    return {
        "event_id": str(uuid.uuid4()),
        "event_type": EVENT_TYPE,
        "event_version": 1,
        "occurred_at": _now(),
        "producer": PRODUCER,
        "payload": p,
    }


class StdoutPublisher:
    """Dry run: one envelope per line."""

    def publish(self, env: dict) -> None:
        sys.stdout.write(json.dumps(env, separators=(",", ":")) + "\n")
        sys.stdout.flush()

    def close(self) -> None:  # pragma: no cover
        pass


class AMQPPublisher:
    """Durable topic exchange, persistent messages, publisher confirms."""

    def __init__(self, url: str):
        self.url = url
        self.conn: pika.BlockingConnection | None = None
        self.ch = None
        self._connect()

    def _connect(self) -> None:
        params = pika.URLParameters(self.url)
        params.heartbeat = 30
        params.blocked_connection_timeout = 30
        self.conn = pika.BlockingConnection(params)
        self.ch = self.conn.channel()
        self.ch.exchange_declare(exchange=EXCHANGE, exchange_type="topic", durable=True)
        self.ch.confirm_delivery()
        log.info("amqp connected exchange=%s", EXCHANGE)

    def publish(self, env: dict) -> None:
        body = json.dumps(env, separators=(",", ":")).encode()
        props = pika.BasicProperties(
            content_type="application/json",
            delivery_mode=2,
            message_id=env["event_id"],
            type=env["event_type"],
            app_id=env["producer"],
            timestamp=int(datetime.now(timezone.utc).timestamp()),
        )
        for attempt in range(3):
            try:
                self.ch.basic_publish(exchange=EXCHANGE, routing_key=env["event_type"], body=body, properties=props, mandatory=False)
                return
            except (pika.exceptions.AMQPError, pika.exceptions.UnroutableError, ConnectionError) as e:
                log.warning("publish failed (attempt %d): %s; reconnecting", attempt + 1, e)
                try:
                    self._connect()
                except Exception as ce:  # noqa: BLE001
                    log.warning("reconnect failed: %s", ce)
        raise RuntimeError(f"could not publish {env['event_id']} after 3 attempts")

    def close(self) -> None:
        try:
            if self.conn and self.conn.is_open:
                self.conn.close()
        except Exception:  # noqa: BLE001
            pass
