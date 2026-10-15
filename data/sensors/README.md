# The sensor catalogue

`data/sensors/<maker>.yml` is the image-sensor catalogue behind
`/cameras/sensors`: one file per maker, read by
`frontend/apps/site/scripts/export-data.mjs` into `src/data/sensors.json`.
Nothing else reads it; the service does not.

Every value is a fact a maker published about a released part. **No datasheet
is hosted or linked from here**, and `sources` holds only addresses anyone can
open: the maker's product page, a public flyer or brief, a press release. A
part that is only on a roadmap, or known only from a document given out under
NDA, is not listed.

## A file

```yaml
name: Sony                                   # as the site shows it
urlname: sony                                # [a-z0-9-]+, the file's name
full_name: Sony Semiconductor Solutions      # optional
website_url: https://www.sony-semicon.com/   # optional
sensors:
- model: IMX307                              # as the maker writes it
  urlname: imx307                            # unique across every file
  aliases: [IMX307LQR, IMX307LQD]            # other spellings boards and ipctool use
  family: STARVIS                            # the maker's line, free text
  status: mp                                 # mp | eol | sampling | unknown
  released: '2017'                           # 'YYYY' or 'YYYY-MM', quoted
  released_basis: press                      # press | flyer | datasheet | estimate
  width: 1945                                # effective pixels
  height: 1109
  megapixels: 2.1                            # only when width/height are not known
  pixel_um: 2.9
  optical_format: '1/2.8'                    # '1/2.8', '1/1.2', '1', '4/3', 'APS-C', '35mm'
  shutter: rolling                           # rolling | global
  cfa: bayer                                 # bayer | rgbir | mono | rccb
  interfaces: [mipi, lvds, dvp]              # mipi | lvds | sublvds | slvs-ec | hispi | dvp
  max_fps: 60                                # at full resolution, linear mode
  adc_bits: 12
  hdr: [dol]                                 # dol | stagger | dcg | clear-hdr | lofic | row-interleave | knee,
                                             # or, when the maker does not name the method:
                                             # multi-exposure | single-frame | unspecified
  nir: false                                 # marketed NIR-enhanced (Nyxel, NIR-enhanced STARVIS, ...)
  category: lowlight                         # optional; overrides the chart column derived on the site
  sensitivity: {value: 2700, unit: mV/lux·s, note: '1/30 s, F5.6, 3200 K'}
  saturation: {value: 1000, unit: mV}        # mV | e-
  snr_max_db: 40
  dr_db: 72                                  # linear, single exposure
  dr_hdr_db: 120                             # best HDR mode
  dark_current: {value: 2.6, unit: mV/s, note: '60 °C'}   # mV/s | e-/s
  twin: ov2735                               # the same silicon sold under another name
  successor: imx662                          # the part the maker replaced it with
  notes: Short English text, one or two sentences.
  sources:
  - {kind: page, url: https://www.sony-semicon.com/en/products/is/security/security.html}
```

Only `model`, `urlname` and `status` are required; leave out what is not
known rather than guess. A spec's unit is the one the maker states
(`mV/lux·s`, `e-/lux·s`, `LSB/lux·s` with the bit depth in `note`); the site
compares like with like only.

The release date is when the part was announced or first shipped. Prefer a
press release (`press`), then a flyer's first copyright year (`flyer`), then the
earliest datasheet revision (`datasheet`); otherwise an estimate from its
family (`estimate`). The site shows everything but `press` as "≈2017".

Run `npm run export -w @openipc/site` in `frontend/` after any change;
the export refuses an unknown key, a word outside a vocabulary, a duplicate
`urlname` or alias, and a source that is not a public `https://` address.
