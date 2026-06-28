FROM node:22-alpine AS dashboard-builder

WORKDIR /app
COPY internal/dashboard/ui/package.json internal/dashboard/ui/package-lock.json ./
RUN npm ci
COPY internal/dashboard/ui/ .
RUN npm run build

FROM golang:1.26-alpine AS builder

RUN apk add --no-cache ca-certificates

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY . .

COPY --from=dashboard-builder /static internal/dashboard/static
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o zeep ./cmd/zeep

FROM scratch

COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /app/zeep /zeep

WORKDIR /app

ENTRYPOINT ["/zeep"]
CMD ["serve"]
