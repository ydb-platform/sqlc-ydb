from pathlib import Path
import unittest


ROOT = Path(__file__).resolve().parents[1]


class ReleaseCheckoutTest(unittest.TestCase):
    def test_repository_has_no_line_ending_policy(self):
        self.assertFalse((ROOT / ".gitattributes").exists())


if __name__ == "__main__":
    unittest.main()
