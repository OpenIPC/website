"""python3 -m unittest discover tools/boards-backup"""
import hashlib
import io
import os
import tarfile
import tempfile
import unittest

import tarpack


def build(fmt, members):
    buf = io.BytesIO()
    with tarfile.open(fileobj=buf, mode="w", format=fmt) as t:
        for name, data, typ in members:
            info = tarfile.TarInfo(name)
            info.type = typ
            if typ == tarfile.SYMTYPE or typ == tarfile.LNKTYPE:
                info.linkname = data.decode()
                t.addfile(info)
            elif typ == tarfile.DIRTYPE:
                t.addfile(info)
            else:
                info.size = len(data)
                t.addfile(info, io.BytesIO(data))
    return buf.getvalue()


SHARED = os.urandom(70000)
MEMBERS = [
    ("files/", b"", tarfile.DIRTYPE),
    ("files/shared.bin", SHARED, tarfile.REGTYPE),
    ("files/empty", b"", tarfile.REGTYPE),
    ("files/block", b"x" * 512, tarfile.REGTYPE),
    ("files/" + "long-name-" * 20 + ".pdf", b"pdf bytes", tarfile.REGTYPE),
    ("files/плата.jpg", os.urandom(1234), tarfile.REGTYPE),
    ("files/link", "shared.bin".encode(), tarfile.SYMTYPE),
    ("files/hard", "files/shared.bin".encode(), tarfile.LNKTYPE),
]


class RoundTrip(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.bucket = os.path.join(self.tmp.name, "bucket")
        self.stage = os.path.join(self.tmp.name, "stage")
        self.store = tarpack.Store("file://" + self.bucket)

    def tearDown(self):
        self.tmp.cleanup()

    def put(self, key, data):
        p = os.path.join(self.bucket, key)
        os.makedirs(os.path.dirname(p), exist_ok=True)
        with open(p, "wb") as f:
            f.write(data)

    def test_every_format_comes_back_byte_identical(self):
        tars = {f"boards-donors/x/{n}.tar": build(fmt, MEMBERS)
                for n, fmt in (("pax", tarfile.PAX_FORMAT), ("gnu", tarfile.GNU_FORMAT))}
        # ustar cannot hold the long name or the Cyrillic one
        tars["boards/ustar.tar"] = build(tarfile.USTAR_FORMAT, MEMBERS[:4] + MEMBERS[6:])
        for k, v in tars.items():
            self.put(k, v)
        known = set()
        for k in tars:
            tarpack.pack(self.store, k, self.stage, known)
        self.store.put_tree(os.path.join(self.stage, "blobs"), tarpack.BLOBS)
        self.store.put_tree(os.path.join(self.stage, "recipes"), tarpack.RECIPES)
        for k, v in tars.items():
            out = io.BytesIO()
            meta = tarpack.unpack(f"{tarpack.RECIPES}/{k}.recipe.gz", None, self.store, out)
            self.assertEqual(out.getvalue(), v, k)
            self.assertEqual(meta["sha256"], hashlib.sha256(v).hexdigest())
        # the same bytes in three tars are stored once
        sums = {k.rsplit("/", 1)[1] for k in self.store.keys(tarpack.BLOBS)}
        self.assertIn(hashlib.sha256(SHARED).hexdigest(), sums)
        self.assertEqual(len(sums), 4)  # shared, block, pdf, jpg; empty files take none

    def test_a_blob_with_the_wrong_bytes_fails(self):
        v = build(tarfile.PAX_FORMAT, MEMBERS)
        self.put("boards/a.tar", v)
        tarpack.pack(self.store, "boards/a.tar", self.stage, set())
        blobs = os.path.join(self.stage, "blobs")
        s = hashlib.sha256(SHARED).hexdigest()
        with open(os.path.join(blobs, s[:2], s), "r+b") as f:
            f.write(b"!")
        with self.assertRaises(SystemExit):
            tarpack.unpack(os.path.join(self.stage, "recipes", "boards/a.tar.recipe.gz"), blobs, self.store,
                           io.BytesIO())

    def test_record_padding_after_the_end_survives(self):
        v = build(tarfile.PAX_FORMAT, MEMBERS[:2]) + bytes(4096)
        self.put("boards/padded.tar", v)
        tarpack.pack(self.store, "boards/padded.tar", self.stage, set())
        out = io.BytesIO()
        tarpack.unpack(os.path.join(self.stage, "recipes", "boards/padded.tar.recipe.gz"),
                       os.path.join(self.stage, "blobs"), self.store, out)
        self.assertEqual(out.getvalue(), v)


if __name__ == "__main__":
    unittest.main()
