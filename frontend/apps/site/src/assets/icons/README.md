# Icons

Bootstrap Icons, MIT, copied one at a time from `node_modules/bootstrap-icons`
as a page needs one.

Copied rather than fetched: the original pages drew these from the bootstrap-icons
webfont, which is a render-blocking request for about 100 KB to draw a chip and
an arrow. A prerendered page that uses thirty glyphs should carry thirty SVGs,
inlined, and every one of these draws with `fill="currentColor"` so a section's
own colour reaches it.

Adding one:

    cp ../../../../../node_modules/bootstrap-icons/icons/<name>.svg .

`src/components/Icon.astro` resolves them by basename and fails the build with
that command in the message when one is missing, so there is nothing to
register.
