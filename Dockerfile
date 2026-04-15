FROM --platform=$BUILDPLATFORM golang:1.25-alpine AS builder

ARG TARGETOS
ARG TARGETARCH

WORKDIR /src

RUN apk add --no-cache build-base

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=1 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -o /out/main ./main.go

FROM alpine:3.22

RUN apk add --no-cache ca-certificates tzdata sqlite-libs && \
    addgroup -S app && adduser -S app -G app

WORKDIR /app

COPY --from=builder /out/main /app/main
COPY --from=builder /src/template /app/template
COPY --from=builder /src/uploads /app/uploads

RUN mkdir -p /app/uploads/alerts /app/data && \
    ln -sf /app/data/cc.db /app/cc.db && \
    chown -R app:app /app

USER app

EXPOSE 8080

VOLUME ["/app/uploads", "/app/data"]

CMD ["/app/main"]
