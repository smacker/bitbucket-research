FROM golang:1.13-alpine3.10 as builder

RUN apk update && \
    apk add ca-certificates git

WORKDIR /bb-server
COPY . .

RUN go build -v -o bb-server .


FROM alpine:3.10

RUN apk update && \
    apk add ca-certificates && \
    rm -rf /var/cache/apk/*

COPY --from=builder /bb-server/bb-server /bin/main
COPY --from=builder /bb-server/migrations /migrations

CMD ["/bin/main"]
