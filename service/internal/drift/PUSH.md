# Pushing the firmware-drift report

OpenIPC/builder's `firmware-drift.yml` checks every day how far builder's devices have drifted from OpenIPC/firmware, and pushes the result here. The firmware explorer shows it per device. It replaced the GitHub issue the workflow used to rewrite, OpenIPC/builder#131.

```
POST https://openipc.org/api/v1/drift
Authorization: Bearer <GitHub Actions OIDC token, audience https://openipc.org>
Content-Type: application/json
Content-Encoding: gzip            (optional)
```

The token is checked like a build push's (`builds/verify.go`):
- **Owner:** the repository must belong to OpenIPC.
- **Events:** pull request events are refused.
- **Workflow:** `job_workflow_ref` must be exactly `OpenIPC/builder/.github/workflows/firmware-drift.yml@refs/heads/master`. A dispatch from another branch is refused, and so is a build workflow pushing this document.
- **Limits:** the body is limited to 4 MB as sent and 16 MB inflated.

The body is `check-firmware-drift.py --json`, schema 1:

```json
{
  "schema": 1,
  "checked_at": "2026-10-04T05:03:11Z",
  "builder_commit": "<40 hex>", "firmware_commit": "<40 hex>",
  "buildroot_version": "2024.02.10",
  "run_url": "https://github.com/OpenIPC/builder/actions/runs/<id>",
  "devices": [{"device": "gk7202v300_lite_xg521", "dir": "gk7202v300_lite_xg521"}],
  "shadows": [{
    "builder": "devices/apfpv/general/overlay/etc/init.d/S40network",
    "firmware": "general/overlay/etc/init.d/S40network",
    "status": "moved",
    "pinned_blob": "<40 hex>", "current_blob": "<40 hex>", "pinned_commit": "<40 hex>",
    "pin_unknown": false, "truncated": false,
    "reconciled": "2026-08-25", "note": "...",
    "devices": ["ssc338q_apfpv", "ssc378qe_apfpv"],
    "commits": [{"sha": "<40 hex>", "date": "2026-09-14T10:00:00+03:00", "author": "...", "subject": "..."}]
  }],
  "symbols": [{"symbol": "BR2_PACKAGE_X", "kind": "firmware_retired", "reason": null,
               "allowed": [], "devices": ["..."]}],
  "notices": []
}
```

**Shadow `status`:**

| Status | Meaning |
|---|---|
| `ok` | reconciled, and firmware's file is still the pinned blob |
| `unpinned` | firmware has a file at this path and nothing records a reconciliation |
| `missing_builder` | the pin names a builder file that is gone |
| `firmware_gone` | firmware no longer has the file |
| `moved` | firmware changed the file since the pin; `commits` are the changes after the pinned commit |

**Symbol `kind`:**

| Kind | Meaning |
|---|---|
| `dead` | no Kconfig declares the symbol |
| `known_dead` | same, already acknowledged |
| `stray` | used outside the devices it is allowlisted for |
| `firmware_retired` | no firmware defconfig selects it |
| `stale_entry` | `firmware-drift.json` lists a symbol nothing selects |

**Validation:** every device a shadow or symbol names must appear in `devices`. Unknown fields are ignored (the checker also sends each symbol's defconfig `users`).

**Storage:** a push is stored as one report. A retried push of the same run attempt replaces its own report, and the newest 60 are kept. Responses: 201 `{"report", "devices", "shadows", "symbols"}`, 400, 401/403, 413 or 5xx.

**Read side:** `GET /api/v1/explorer/builder/upstream` returns the newest report:
- every device with counts of shadowed files, ones needing attention, and symbol findings;
- each open finding's `since`: the oldest report in the unbroken run of retained reports that had it.
