# syntax=docker/dockerfile:1.7

FROM golang:1.27-alpine AS build
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build \
    -trimpath \
    -ldflags="-s -w" \
    -o /out/mailnotifier \
    ./cmd/mailnotifier \
    && mkdir -p /out/data

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/mailnotifier /mailnotifier
COPY --from=build --chown=65532:65532 /out/data /data

VOLUME ["/data"]
EXPOSE 2525 8080

ENTRYPOINT ["/mailnotifier"]
