# syntax=docker/dockerfile:1

# ---- build stage ------------------------------------------------------
FROM golang:1.25-alpine AS build

WORKDIR /src

# Cached separately from the rest of the source so a code-only change
# doesn't re-download the whole module graph.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# CGO_ENABLED=0 + a fully static distroless runtime below: pgx and colly
# are pure Go, nothing here needs cgo. -trimpath drops local build paths
# from the binary; -s -w strips the symbol table/DWARF debug info — none
# of that is needed at runtime and both meaningfully shrink the binary.
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -trimpath -ldflags="-s -w" -o /out/server ./cmd/server

# ---- runtime stage ------------------------------------------------------
# distroless/static: no shell, no package manager, just the CA bundle and
# a nonroot user — the smallest attack surface for a statically linked Go
# binary that only needs outbound TLS (Kalibrr) and to listen on a port.
FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=build /out/server /server

USER nonroot:nonroot

# Render assigns its own PORT (not always 8080) and reaches the container
# on it directly — see internal/config, which already reads PORT from the
# environment. EXPOSE is documentation only, not a hard binding.
EXPOSE 8080

ENTRYPOINT ["/server"]
