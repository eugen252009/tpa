import unittest
from validate import members, control


class OracleTests(unittest.TestCase):
    def test_reject_non_archive(self):
        with self.assertRaises(ValueError):
            members(b'not an archive')

    def test_reject_short_header(self):
        with self.assertRaises(ValueError):
            members(b'!<arch>\nshort')

    def test_reject_duplicate_control(self):
        with self.assertRaises(ValueError):
            control('Package: first\nPackage: second\n')

    def test_preserve_dependencies_and_continuations(self):
        self.assertEqual(control('Package: meta-00000\nDepends: dpkg (>= 1.0)\nDescription: first\n second\n'),
                         {'Package': 'meta-00000', 'Depends': 'dpkg (>= 1.0)', 'Description': 'first\n second'})


if __name__ == '__main__':
    unittest.main()
