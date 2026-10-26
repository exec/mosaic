import unittest
from release_notes import release_notes


class ReleaseNotesTests(unittest.TestCase):
    def test_exact_version_includes_all_sections_and_stops_before_next_release(self):
        changelog = "# Changelog\n\n## 1.9.0 — today\n\nFeatures\n\n### Limits\nMigration details\n\n## 1.8.1 — earlier\nOld notes\n"
        result = release_notes(changelog, "v1.9.0")
        self.assertIn("# Mosaic v1.9.0", result)
        self.assertIn("Features\n\n### Limits\nMigration details", result)
        self.assertIn("SHA256SUMS", result)
        self.assertNotIn("Old notes", result)
        self.assertEqual(result, release_notes(changelog, "1.9.0"))

    def test_missing_or_empty_entry_fails_instead_of_publishing_incomplete_notes(self):
        for changelog in ["## 1.8.1 — earlier\nOld notes", "## 1.9.0 — today\n\n## 1.8.1 — earlier\nOld notes"]:
            with self.assertRaises(ValueError):
                release_notes(changelog, "v1.9.0")
