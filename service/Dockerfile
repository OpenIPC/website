# The Go service's image: ghcr.io/openipc/website-go:<sha>.
#
# Built from the repository root (the catalogue lives in data/) by the Actions
# `build` job, tagged with the commit it was built from.
#
# The runtime is Debian bookworm with libvips 8.14 from Debian, the same
# libvips the wall's variants were first proven byte-identical on (#295), and a libvips
# upgrade is a change to what the wall looks like. libheif decodes the cameras
# that send HEIF. The binary itself is static (CGO_ENABLED=0) and shells out
# to `vips`.

FROM golang:1.27.1 AS build
WORKDIR /src/service
COPY service/go.mod service/go.sum ./
RUN go mod download
COPY service/ ./
ARG REVISION=dev
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=${REVISION}" -o /openipc ./cmd/openipc

FROM debian:bookworm-slim
RUN apt-get update -qq \
 && apt-get install -y --no-install-recommends ca-certificates curl git libvips-tools libheif1 \
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
