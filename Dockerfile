# resvg rasterizes the SVG drawings of the vault; Alpine does not package
# it, so it is built here as a static binary.
FROM rust:1.89-alpine AS resvg
RUN apk add --no-cache musl-dev \
 && cargo install resvg --version 0.45.1 --locked --root /out

FROM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/terakoya ./cmd/terakoya

FROM alpine:3.22
# font-dejavu: the fonts drawings are rendered with (Latin, Greek, Cyrillic).
RUN apk add --no-cache ca-certificates git tzdata font-dejavu
COPY --from=resvg /out/bin/resvg /usr/local/bin/resvg
COPY --from=build /out/terakoya /usr/local/bin/terakoya
ENV TERAKOYA_REPO_DIR=/srv/vault \
    TERAKOYA_DATA_DIR=/srv/data \
    TERAKOYA_FONTS_DIR=/usr/share/fonts/dejavu
WORKDIR /srv
ENTRYPOINT ["terakoya"]
CMD ["serve"]
