"""Capture JFTech's product catalogue: where Xiongmai's products continue.

  python jftech.py <capture-dir>

xiongmaitech.com stopped at the 2023 line; the same product line -- the same
module codes, firmware on the same download servers -- carries on under the
JFTech brand at jftech.com, a single-page app reading a JSON API:

  POST https://en.jftech.com/api-portal/product/categoryTree.json  {}
  POST .../product/productList.json    {"categoryId": N, "page": P, "limit": L}
  POST .../product/productDetail.json  {"productId": N}

The API answers only HTTP/2 (an HTTP/1.1 client is redirected to plain http,
which breaks the POST), so those calls go through curl --http2; the photos
and parameter sheets they link (en-static.jftech.com) are ordinary GETs.

What it keeps, in the layout capture.py uses: every API answer and every
linked file, logged with its sha256, and api.json -- the category tree and,
per product, its detail and the leaf categories that list it. What becomes
what in the catalogue is decided by the snapshot builder, not here.
"""

import argparse
import hashlib
import json
import os
import subprocess
import sys
import time

from capture import Capture, say

API = "https://en.jftech.com/api-portal/product/"
PAGE = 50


class Api:
    """POSTs over HTTP/2, one a second, each answer logged like a capture."""

    def __init__(self, cap, delay):
        self.cap, self.delay, self._last = cap, delay, 0.0

    def post(self, path, body):
        wait = self.delay - (time.monotonic() - self._last)
        if wait > 0:
            time.sleep(wait)
        self._last = time.monotonic()
        url = API + path
        data = json.dumps(body, sort_keys=True)
        for attempt in range(3):
            r = subprocess.run(["curl", "-s", "--http2", "--max-time", "60", "-A", "Mozilla/5.0",
                                "-H", "Content-Type: application/json", "-X", "POST", "-d", data,
                                "-w", "\n%{http_code}", url], capture_output=True)
            raw, _, status = r.stdout.rpartition(b"\n")
            if status == b"200":
                try:
                    doc = json.loads(raw)
                except ValueError:
                    doc = None
                if doc and doc.get("code") == 2000:
                    self.record(url, data, raw)
                    return doc["data"]
            time.sleep(5 * (attempt + 1))
        say(f"  {status.decode() or 'no answer'} {path} {data}")
        return None

    def record(self, url, data, raw):
        sha = hashlib.sha256(raw).hexdigest()
        path = os.path.join(self.cap.root, "files", sha)
        if not os.path.exists(path):
            with open(path, "wb") as f:
                f.write(raw)
        entry = {"url": url, "body": data, "status": 200, "type": "application/json", "bytes": len(raw),
                 "sha256": sha, "file": "files/" + sha, "fetched_at": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
                 "via": "direct"}
        with open(self.cap.log_path, "a") as f:
            f.write(json.dumps(entry, ensure_ascii=False) + "\n")


def leaves(tree, path=()):
    for n in tree or []:
        here = path + (n["name"].strip(),)
        if n.get("children"):
            yield from leaves(n["children"], here)
        else:
            yield n["id"], here


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("out")
    ap.add_argument("--delay", type=float, default=1.0)
    args = ap.parse_args()
    cap = Capture(args.out, delay=args.delay)
    api = Api(cap, args.delay)
    tree = api.post("categoryTree.json", {})
    if tree is None:
        sys.exit("no category tree")
    products, listed, failed = {}, {}, []
    for cid, path in leaves(tree):
        page, seen = 1, 0
        while True:
            got = api.post("productList.json", {"categoryId": cid, "page": page, "limit": PAGE})
            if got is None:
                # A failed request is not the end of a list: the capture is short.
                failed.append(f"category {cid} page {page}")
                break
            rows = got.get("data") or []
            seen += len(rows)
            for p in rows:
                listed.setdefault(str(p["id"]), []).append({"id": cid, "path": list(path)})
            if not rows or page * PAGE >= (got.get("total") or 0):
                if seen < (got.get("total") or 0):
                    failed.append(f"category {cid}: {seen} of {got.get('total')} listed")
                break
            page += 1
    say(f"{len(listed)} products in {sum(1 for _ in leaves(tree))} leaf categories")
    files = 0
    for pid in sorted(listed, key=int):
        d = api.post("productDetail.json", {"productId": int(pid)})
        if d is None:
            failed.append(f"product {pid}")
            continue
        products[pid] = d
        for f in (d.get("detailImageList") or []) + (d.get("detailDocList") or []):
            url = (f or {}).get("httpAddr")
            if url:
                e, body = cap.get(url)
                files += body is not None
                if body is None:
                    say(f"  {e.get('status')} {url}")
                    failed.append(url)
    if failed:
        # api.json is what the snapshot builder trusts; a short capture must
        # not become one. Rerun it (the files already fetched are not fetched
        # again; the API answers are).
        for f in failed[:20]:
            say(f"  missing: {f}")
        sys.exit(f"{len(failed)} requests failed; api.json not written")
    with open(os.path.join(args.out, "api.json"), "w") as f:
        json.dump({"tree": tree, "products": products, "listed": listed}, f, ensure_ascii=False, indent=1)
    say(f"{len(products)} products, {files} files")


if __name__ == "__main__":
    main()
