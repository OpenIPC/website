# Turns donor documents into what the catalogue publishes: .doc/.docx to PDF
# (LibreOffice Writer), PDF pages to PNG (poppler). Noto CJK because most of
# Xiongmai's interface descriptions are Chinese, or Chinese-labelled drawings.
#
#   docker build -t board-render:1 tools/board-donors/render
FROM debian:trixie-slim
RUN apt-get update && DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends \
      libreoffice-writer-nogui fonts-noto-cjk fonts-liberation2 poppler-utils python3 python3-pil \
    && rm -rf /var/lib/apt/lists/*
ENV HOME=/tmp
