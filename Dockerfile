FROM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/terakoya ./cmd/terakoya

FROM alpine:3.22
RUN apk add --no-cache ca-certificates git tzdata
COPY --from=build /out/terakoya /usr/local/bin/terakoya
ENV TERAKOYA_REPO_DIR=/srv/content \
    TERAKOYA_DATA_DIR=/srv/data
WORKDIR /srv
ENTRYPOINT ["terakoya"]
CMD ["serve"]
