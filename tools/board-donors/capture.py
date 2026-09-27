"""Shared capture machinery for the board donors: a polite, resumable fetcher
that keeps every response as it came, with a log line per URL.

Layout of a capture directory:

  log.jsonl          one line per fetched URL: url, final_url, status, type,
                     bytes, sha256, file, fetched_at, via (direct | wayback)
  files/<sha256>     the body, stored once by content
"""

import hashlib
import json
import os
import socket
import ssl
import sys
import time
import urllib.error
import urllib.parse
import urllib.request

UA = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/128.0 Safari/537.36"


class Capture:
    def __init__(self, root, insecure_hosts=(), delay=1.0):
        self.root = root
        self.delay = delay
        self.insecure_hosts = set(insecure_hosts)
        os.makedirs(os.path.join(root, "files"), exist_ok=True)
        self.log_path = os.path.join(root, "log.jsonl")
        self.seen = {}
        if os.path.exists(self.log_path):
            with open(self.log_path) as f:
                for line in f:
                    e = json.loads(line)
                    self.seen[e["url"]] = e
        self._last = 0.0
        # Hosts whose names no longer resolve: asked once, then skipped.
        self.dead_hosts = set()
        # Wayback misses per original host; after three in a row, stop asking.
        self.wayback_misses = {}
        self._unverified = ssl.create_default_context()
        self._unverified.check_hostname = False
        self._unverified.verify_mode = ssl.CERT_NONE

    def _context(self, url):
        host = urllib.parse.urlsplit(url).hostname or ""
        # Verification is off only for the vendor hosts whose certificates
        # have lapsed; everything else, the Wayback Machine included, is checked.
        if any(host == h or host.endswith("." + h) for h in self.insecure_hosts):
            return self._unverified
        return None

    def _wait(self):
        dt = time.monotonic() - self._last
        if dt < self.delay:
            time.sleep(self.delay - dt)
        self._last = time.monotonic()

    def get(self, url, headers=None, wayback=False, refetch=False):
        """The body of url, fetched once per capture. Returns (entry, bytes) or
        (entry, None) when there is no body worth keeping."""
        if not refetch and url in self.seen and self.seen[url]["status"] == 200:
            e = self.seen[url]
            return e, self.read(e)
        entry, body = self._fetch(url, headers)
        host = urllib.parse.urlsplit(url).hostname or ""
        if entry["status"] != 200 and wayback and self.wayback_misses.get(host, 0) < 3:
            wb = "https://web.archive.org/web/2026id_/" + url
            e2, b2 = self._fetch(wb, headers)
            if e2["status"] == 200:
                e2["url"], e2["via"] = url, "wayback"
                e2["wayback_url"] = wb
                entry, body = e2, b2
                self.wayback_misses[host] = 0
            else:
                self.wayback_misses[host] = self.wayback_misses.get(host, 0) + 1
                entry["wayback"] = f"miss ({e2.get('status')})"
        self._record(entry)
        return entry, body

    def _fetch(self, url, headers):
        host = urllib.parse.urlsplit(url).hostname or ""
        entry = {"url": url, "fetched_at": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()), "via": "direct"}
        if host in self.dead_hosts:
            entry.update(status=0, error="host does not resolve (seen earlier in this run)")
            return entry, None
        self._wait()
        # Vendor paths carry Chinese directory names; urllib wants them encoded.
        wire = urllib.parse.quote(url, safe=":/?&=%#@+,;~!$'()*[]")
        req = urllib.request.Request(wire, headers={"User-Agent": UA, **(headers or {})})
        for attempt in range(3):
            try:
                with urllib.request.urlopen(req, timeout=60, context=self._context(url)) as r:
                    body = r.read()
                    entry.update(status=r.status, final_url=r.geturl(), type=r.headers.get("Content-Type", ""))
                    break
            except urllib.error.HTTPError as e:
                entry.update(status=e.code, final_url=url, type=e.headers.get("Content-Type", "") if e.headers else "")
                body = None
                if e.code < 500:
                    break
            except Exception as e:  # DNS, TLS, timeouts
                entry.update(status=0, error=f"{type(e).__name__}: {e}")
                body = None
                # A name that does not resolve will not start resolving between
                # retries; the vendor's dead zones cost 18 s a file otherwise.
                if isinstance(getattr(e, "reason", None), socket.gaierror):
                    self.dead_hosts.add(host)
                    break
            time.sleep(3 * (attempt + 1))
        if body is not None:
            sha = hashlib.sha256(body).hexdigest()
            path = os.path.join(self.root, "files", sha)
            if not os.path.exists(path):
                with open(path + ".tmp", "wb") as f:
                    f.write(body)
                os.replace(path + ".tmp", path)
            entry.update(bytes=len(body), sha256=sha, file="files/" + sha)
        return entry, body

    def _record(self, entry):
        self.seen[entry["url"]] = entry
        with open(self.log_path, "a") as f:
            f.write(json.dumps(entry, ensure_ascii=False) + "\n")

    def read(self, entry):
        if not entry.get("file"):
            return None
        with open(os.path.join(self.root, entry["file"]), "rb") as f:
            return f.read()


def say(*a):
    print(*a, file=sys.stderr, flush=True)
