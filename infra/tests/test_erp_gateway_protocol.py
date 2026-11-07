from pathlib import Path
import sys
import unittest

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "linux"))
from erp_gateway_protocol import parse_request


class ERPGatewayProtocolTest(unittest.TestCase):
    def test_status_and_digest_are_the_only_requests(self):
        revision = "a" * 40
        digest = "sha256:" + "b" * 64
        self.assertEqual(parse_request("status"), ("status",))
        self.assertEqual(parse_request(f"deploy {revision} {digest}"), ("deploy", revision, digest))
        for text in (
            "", "bash", "status\n", "status --help",
            f"deploy {revision} {digest}; id",
            f"deploy {revision} {digest}\nstatus",
            f"deploy  {revision} {digest}",
            f"deploy {revision} ghcr.io/another/package@{digest}",
            f"deploy {revision} {digest} --mount /",
            "deploy main latest", "a" * 1024,
        ):
            with self.subTest(text=text), self.assertRaises(ValueError):
                parse_request(text)


if __name__ == "__main__":
    unittest.main()
