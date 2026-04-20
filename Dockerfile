FROM golang:1.26-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /fleet-dispatch ./cmd/server

FROM alpine:3.19
RUN apk add --no-cache ca-certificates tzdata
WORKDIR /app
COPY --from=builder /fleet-dispatch /app/fleet-dispatch
COPY web/ /app/web/
EXPOSE 8080
CMD ["/app/fleet-dispatch"]
