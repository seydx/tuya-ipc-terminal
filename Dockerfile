FROM golang:1.23-alpine AS builder

RUN apk add --no-cache git bash expect

WORKDIR /app

RUN git clone https://github.com/seydx/tuya-ipc-terminal

WORKDIR /app/tuya-ipc-terminal

RUN chmod +x build.sh
RUN ./build.sh

FROM alpine:latest

run apk add --no-cache bash expect

RUN addgroup -g 1000 -S appgroup && adduser -u 1000 -S appuser -G appgroup

WORKDIR /app

RUN mkdir -p /app/.tuya-data
RUN chown -R appuser:appgroup /app/.tuya-data

USER appuser

COPY --from=builder /app/tuya-ipc-terminal/tuya-ipc-terminal /app/

EXPOSE 8554

CMD ["./tuya-ipc-terminal", "rtsp", "start", "--port", "8554"]
