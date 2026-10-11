from __future__ import annotations

import unittest

from check_compose import validate


class ComposeChecksTests(unittest.TestCase):
    def test_preserves_existing_static_contract(self) -> None:
        rendered = """
host_ip: 127.0.0.1
services:
  admin-frontend:
    image: gopulse/admin-frontend:2.5.6
  frontend:
    image: gopulse/frontend:2.5.6
  backend:
    image: gopulse/backend:2.5.6
  business-worker:
    image: gopulse/business-worker:2.5.6
  search-indexer:
    image: gopulse/search-indexer:2.5.6
  router:
    image: gopulse/router:2.5.6
  marshaller:
    image: gopulse/marshaller:2.5.6
  monitor:
    image: gopulse/monitor:2.5.6
  kafka:
    image: apache/kafka:4.3.1
  victoriametrics:
    image: victoriametrics/victoria-metrics:v1.151.0
    condition: service_completed_successfully
networks:
  business:
    internal: true
  observe:
    internal: true
"""
        self.assertEqual(validate(rendered, "2.5.6"), [])

    def test_reports_missing_contract(self) -> None:
        self.assertTrue(validate("services: {}\n", "2.5.6"))


if __name__ == "__main__":
    unittest.main()
