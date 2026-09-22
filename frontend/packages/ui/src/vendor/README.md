Third-party source, carried verbatim. Not ours to restyle, so it is outside
the lint gate (`eslint.config.ts` ignores this directory) — but it is inside
the type-check and the build, because a vendored file that stops compiling is
still a broken build.

| file | origin | licence |
|---|---|---|
| `qrcodegen.ts` | [Project Nayuki, QR Code generator library](https://www.nayuki.io/page/qr-code-generator-library) | MIT, header retained |
