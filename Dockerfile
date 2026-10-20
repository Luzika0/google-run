
FROM golang:1.23-alpine AS builder

WORKDIR /app
COPY go.mod ./
COPY main.go ./

RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /proxy ./main.go

FROM alpine:3.22

RUN adduser -D -H -u 10001 appuser
COPY --from=builder /proxy /proxy

USER appuser
EXPOSE 8080
ENTRYPOINT ["/proxy"]
