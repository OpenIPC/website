#!/usr/bin/env python3
"""Keep tars in the backup bucket with every file's bytes stored once.

A board set and a donor snapshot hold mostly the same photos, pinouts and
flash dumps, and each new set or review round of a snapshot used to go up as
a whole tar again. `pack` splits a tar into its files' contents, stored once
each under boards/sha256/<ab>/<sum> (written once, never overwritten), and a
recipe under boards/recipes/<key>.recipe.gz: the tar's headers and padding,
verbatim, and the blobs between them in order. `unpack` concatenates them
back into the same bytes -- the same sha256 -- so a snapshot still matches
its pin in service/internal/boards/snapshot.go.

    tarpack.py pack   --store s3://openipc-org-backup --stage DIR KEY...
    tarpack.py unpack --blobs s3://openipc-org-backup|DIR RECIPE [-o OUT]
    tarpack.py verify --blobs s3://openipc-org-backup|DIR RECIPE...

RECIPE is a local file, or a key in the bucket (boards/recipes/...). With
--blobs DIR the blobs are read from a local copy of boards/sha256/ (as
`aws s3 sync s3://<bucket>/boards/sha256/ DIR` leaves it), one request for
the lot instead of one per file. Standard library and the aws CLI only, like
the deploy scripts; the CLI takes its credentials and endpoint from the
environment as usual.
"""
import argparse
import gzip
import hashlib
import json
import os
import shutil
import subprocess
import sys
import tempfile

BLOBS = "boards/sha256"
RECIPES = "boards/recipes"
# Regular files (and contiguous ones) carry the bytes worth keeping once;
# everything else in a tar is headers, link targets, long names and padding,
# kept in the recipe as it is.
REGULAR = (b"0", b"\0", b"7")
ZERO = bytes(512)
CHUNK = 1 << 20


def blob_key(sum_):
    return f"{BLOBS}/{sum_[:2]}/{sum_}"


class Store:
    """A bucket (s3://name) or, for the tests, a directory standing in for one."""

    def __init__(self, url):
        if url.startswith("s3://"):
            self.bucket, self.root = url[5:].strip("/"), None
        else:
            self.bucket, self.root = None, url[7:] if url.startswith("file://") else url

    def keys(self, prefix):
        if self.root is not None:
            base = os.path.join(self.root, prefix)
            return {os.path.relpath(os.path.join(d, f), self.root)
                    for d, _, fs in os.walk(base) for f in fs}
        out = subprocess.run(["aws", "s3api", "list-objects-v2", "--bucket", self.bucket,
                              "--prefix", prefix + "/", "--query", "Contents[].Key", "--output", "json"],
                             check=True, capture_output=True, text=True).stdout
        return set(json.loads(out) or [])

    def open(self, key):
        """A readable stream of the object, and a function that checks it was read whole."""
        if self.root is not None:
            f = open(os.path.join(self.root, key), "rb")
            return f, f.close
        p = subprocess.Popen(["aws", "s3", "cp", "--only-show-errors", f"s3://{self.bucket}/{key}", "-"],
                             stdout=subprocess.PIPE)

        def done():
            p.stdout.close()
            if p.wait():
                raise SystemExit(f"reading s3://{self.bucket}/{key} failed")
        return p.stdout, done

    def put_tree(self, local, prefix):
        """Upload every file under local to prefix/<its path>."""
        if not any(fs for _, _, fs in os.walk(local)):
            return
        if self.root is not None:
            for d, _, fs in os.walk(local):
                for f in fs:
                    dst = os.path.join(self.root, prefix, os.path.relpath(os.path.join(d, f), local))
                    os.makedirs(os.path.dirname(dst), exist_ok=True)
                    shutil.copyfile(os.path.join(d, f), dst)
            return
        subprocess.run(["aws", "s3", "cp", "--only-show-errors", "--recursive", local,
                        f"s3://{self.bucket}/{prefix}/"], check=True)


def read_exact(f, n):
    buf = bytearray()
    while len(buf) < n:
        b = f.read(n - len(buf))
        if not b:
            break
        buf += b
    return bytes(buf)


def octal(field):
    if field[0] & 0x80:  # base-256, for sizes past 8 GiB
        return int.from_bytes(bytes([field[0] & 0x7F]) + field[1:], "big")
    field = field.strip(b"\0 ")
    return int(field, 8) if field else 0


def pax_size(data):
    size = None
    for rec in data.split(b"\n"):
        _, _, kv = rec.partition(b" ")
        k, _, v = kv.partition(b"=")
        if k == b"size":
            size = int(v)
    return size


class Hashing:
    def __init__(self, f):
        self.f, self.h, self.n = f, hashlib.sha256(), 0

    def read(self, n):
        b = read_exact(self.f, n)
        self.h.update(b)
        self.n += len(b)
        return b


def pack(store, key, stage, known):
    """Split one tar into blobs staged under stage/blobs and a recipe under stage/recipes."""
    os.makedirs(stage, exist_ok=True)
    raw, done = store.open(key)
    src = Hashing(raw)
    segs, lit = [], bytearray()
    next_size, new = None, 0

    def blob(n):
        nonlocal new
        h = hashlib.sha256()
        with tempfile.NamedTemporaryFile(dir=stage, delete=False) as t:
            left = n
            while left:
                b = src.read(min(CHUNK, left))
                if not b:
                    raise SystemExit(f"{key}: truncated inside a file")
                h.update(b)
                t.write(b)
                left -= len(b)
        s = h.hexdigest()
        dst = os.path.join(stage, "blobs", s[:2], s)
        if s in known:
            os.unlink(t.name)
        else:
            os.makedirs(os.path.dirname(dst), exist_ok=True)
            os.replace(t.name, dst)
            known.add(s)
            new += n
        return s

    while True:
        hdr = src.read(512)
        if not hdr:
            break
        lit += hdr
        if hdr == ZERO or len(hdr) < 512:
            # The end-of-archive blocks and whatever record padding follows.
            while b := src.read(CHUNK):
                lit += b
            break
        typ = hdr[156:157]
        size = next_size if next_size is not None else octal(hdr[124:136])
        next_size = None
        pad = -size % 512
        if typ in REGULAR and size:
            segs.append(("L", bytes(lit)))
            lit = bytearray()
            segs.append(("B", blob(size), size))
            lit += src.read(pad)
        else:
            data = src.read(size + pad)
            lit += data
            if typ == b"x":
                next_size = pax_size(data[:size])
    segs.append(("L", bytes(lit)))
    done()

    out = os.path.join(stage, "recipes", key + ".recipe.gz")
    os.makedirs(os.path.dirname(out), exist_ok=True)
    with gzip.open(out, "wb") as r:
        r.write(json.dumps({"version": 1, "source": key, "size": src.n,
                            "sha256": src.h.hexdigest()}).encode() + b"\n")
        for s in segs:
            if s[0] == "L":
                if s[1]:
                    r.write(b"L %d\n" % len(s[1]) + s[1])
            else:
                r.write(b"B %s %d\n" % (s[1].encode(), s[2]))
    return src.n, src.h.hexdigest(), new


def segments(r):
    while line := r.readline():
        kind, *rest = line.split()
        if kind == b"L":
            yield "L", read_exact(r, int(rest[0]))
        else:
            yield "B", rest[0].decode(), int(rest[1])


def unpack(recipe, blobs, store, out):
    """Write the tar a recipe describes to out; fail unless it is the original's bytes."""
    f, done = (open(recipe, "rb"), None) if os.path.exists(recipe) else store.open(recipe)
    with gzip.open(f) as r:
        meta = json.loads(r.readline())
        whole, n = hashlib.sha256(), 0
        for seg in segments(r):
            if seg[0] == "L":
                whole.update(seg[1])
                out.write(seg[1])
                n += len(seg[1])
                continue
            _, s, size = seg
            src, close = (open(os.path.join(blobs, s[:2], s), "rb"), None) if blobs else store.open(blob_key(s))
            h, got = hashlib.sha256(), 0
            while b := src.read(CHUNK):
                h.update(b)
                whole.update(b)
                out.write(b)
                got += len(b)
            (close or src.close)()
            if h.hexdigest() != s or got != size:
                raise SystemExit(f"blob {s} does not hold the bytes its name says")
            n += size
    if done:
        done()
    if n != meta["size"] or whole.hexdigest() != meta["sha256"]:
        raise SystemExit(f"{meta['source']}: rebuilt {n} bytes, sha256 {whole.hexdigest()}; "
                         f"want {meta['size']}, {meta['sha256']}")
    return meta


class Discard:
    def write(self, b):
        pass


def main():
    ap = argparse.ArgumentParser(description=__doc__.split("\n\n")[0])
    sub = ap.add_subparsers(dest="cmd", required=True)
    p = sub.add_parser("pack")
    p.add_argument("--store", required=True)
    p.add_argument("--stage", required=True, help="scratch directory, big enough for the new blobs")
    p.add_argument("--no-upload", action="store_true")
    p.add_argument("keys", nargs="+")
    for name in ("unpack", "verify"):
        p = sub.add_parser(name)
        p.add_argument("--store", default="s3://openipc-org-backup")
        p.add_argument("--blobs", help="local copy of boards/sha256/ (default: fetch each blob)")
        p.add_argument("recipes", nargs=1 if name == "unpack" else "+")
        if name == "unpack":
            p.add_argument("-o", "--out")
    a = ap.parse_args()

    if a.cmd == "pack":
        store = Store(a.store)
        known = {k.rsplit("/", 1)[1] for k in store.keys(BLOBS)}
        print(f"{len(known)} blobs already stored", file=sys.stderr)
        for key in a.keys:
            size, sha, new = pack(store, key, a.stage, known)
            print(f"{key}: {size} bytes, sha256 {sha}, {new} bytes of it new", file=sys.stderr)
        if not a.no_upload:
            # Blobs first: a recipe in the bucket never names a blob that is not.
            store.put_tree(os.path.join(a.stage, "blobs"), BLOBS)
            store.put_tree(os.path.join(a.stage, "recipes"), RECIPES)
        return

    store = Store(a.store)
    if a.cmd == "unpack":
        with open(a.out, "wb") if a.out else sys.stdout.buffer as out:
            unpack(a.recipes[0], a.blobs, store, out)
        return
    bad = 0
    for rec in a.recipes:
        try:
            meta = unpack(rec, a.blobs, store, Discard())
            print(f"ok {meta['sha256']} {meta['source']}")
        except SystemExit as e:
            print(f"FAIL {rec}: {e}")
            bad += 1
    sys.exit(1 if bad else 0)


if __name__ == "__main__":
    main()
