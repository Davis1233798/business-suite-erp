"""Contracts for the image identity accepted by the ERP deploy helper."""

from pathlib import Path
import sys
import unittest

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "linux"))
from erp_image_release import release_reference, verified_image_id


REVISION = "a" * 40
DIGEST = "sha256:" + "b" * 64
REFERENCE = "ghcr.io/davis1233798/business-suite-erp-station@" + DIGEST
IMAGE_ID = "sha256:" + "c" * 64


class ReleaseImageTest(unittest.TestCase):
    def test_only_a_digest_in_the_fixed_erp_package_is_accepted(self):
        self.assertEqual(release_reference(REVISION, DIGEST), REFERENCE)
        for digest in (REFERENCE, "latest", "sha256:" + "b" * 63, DIGEST + " ", DIGEST + "\n"):
            with self.subTest(digest=digest), self.assertRaises(ValueError):
                release_reference(REVISION, digest)
        for revision in ("main", REVISION.upper(), REVISION + "\n"):
            with self.subTest(revision=revision), self.assertRaises(ValueError):
                release_reference(revision, DIGEST)

    def test_requires_the_erp_manifest_digest_platform_and_revision(self):
        metadata = {
            "Id": IMAGE_ID, "Os": "linux", "Architecture": "arm64",
            "RepoDigests": [REFERENCE],
            "Config": {"Labels": {"org.opencontainers.image.revision": REVISION}},
        }
        self.assertEqual(verified_image_id(REVISION, DIGEST, metadata), IMAGE_ID)
        invalid = (
            {"Architecture": "amd64"},
            {"Os": "windows"},
            {"RepoDigests": ["ghcr.io/davis1233798/business-suite-mes-station@" + DIGEST]},
            {"RepoDigests": [REFERENCE[:-1] + "0"]},
            {"Config": {"Labels": {"org.opencontainers.image.revision": "d" * 40}}},
            {"Id": "latest"},
        )
        for changes in invalid:
            with self.subTest(changes=changes), self.assertRaises(ValueError):
                verified_image_id(REVISION, DIGEST, {**metadata, **changes})


if __name__ == "__main__":
    unittest.main()
