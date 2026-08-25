# The image tools/webui-gallery runs in. Built by run.sh; nothing else uses it.
#
# Branded Chrome, not Playwright's bundled Chromium: the Preview page decodes
# H.264/H.265 over MSE, and Chromium ships without proprietary codecs, so it
# silently falls back to the MJPEG path and photographs a badge that no ordinary
# user would ever see. The failure is invisible unless you know to look at it.
FROM mcr.microsoft.com/playwright:v1.49.0-noble

# cwebp turns the captures into the two sizes the gallery ships.
RUN apt-get update \
 && apt-get install -y --no-install-recommends webp \
 && rm -rf /var/lib/apt/lists/*

RUN npx playwright install chrome
