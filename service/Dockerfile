# The Go service's image: ghcr.io/openipc/website-go:<sha>.
#
# Built from the repository root (the catalogue lives in data/), by the same
# Actions job and from the same commit as the Rails image, so one SHA names
# both halves of a release.
#
# The runtime is Debian bookworm with libvips 8.14 from Debian, the SAME
# libvips the Rails image carries: that is what makes the wall's variants
# byte-identical between the two (tools/variants-compare.sh), and libheif is
# what decodes the cameras that send HEIF. The binary itself is static
# (CGO_ENABLED=0) and shells out to `vips`.

FROM golang:1.27.1 AS build
WORKDIR /src/service
COPY service/go.mod service/go.sum ./
RUN go mod download
COPY service/ ./
ARG REVISION=dev
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=${REVISION}" -o /openipc ./cmd/openipc

FROM debian:bookworm-slim
RUN apt-get update -qq \
 && apt-get install -y --no-install-recommends ca-certificates curl libvips-tools libheif1 \
 && rm -rf /var/lib/apt/lists/* \
 && groupadd --gid 1000 openipc \
 && useradd --uid 1000 --gid 1000 --no-create-home --shell /usr/sbin/nologin openipc
COPY --from=build /openipc /usr/local/bin/openipc
COPY data/catalogue /app/catalogue
USER 1000:1000
ENV CATALOGUE_DIR=/app/catalogue
EXPOSE 3002 3003
ENTRYPOINT ["openipc"]
CMD ["serve", "--role", "web"]
