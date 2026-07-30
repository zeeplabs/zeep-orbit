# Both build stages are pinned to --platform=$BUILDPLATFORM: neither actually
# needs to run under the target architecture's QEMU emulation. The dashboard
# build produces architecture-independent static JS/CSS; the Go build
# cross-compiles natively via GOARCH=$TARGETARCH. Without this pin, buildx
# runs both stages once per --platform, so an arm64 target build ran `npm ci`
# and `go build` under full QEMU emulation — needlessly slow (npm's
# filesystem-heavy install is especially bad under emulation).
FROM --platform=$BUILDPLATFORM node:22-alpine AS dashboard-builder

WORKDIR /app
COPY internal/dashboard/ui/package.json internal/dashboard/ui/package-lock.json ./
RUN npm ci
COPY internal/dashboard/ui/ .
RUN npm run build

FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS builder

ARG TARGETARCH

RUN apk add --no-cache ca-certificates

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY . .

COPY --from=dashboard-builder /static internal/dashboard/static
RUN CGO_ENABLED=0 GOOS=linux GOARCH=$TARGETARCH go build -ldflags="-s -w" -o zeep ./cmd/zeep

FROM scratch

COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /app/zeep /zeep

WORKDIR /app

ENTRYPOINT ["/zeep"]
CMD ["serve"]
