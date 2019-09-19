FROM golang:1.13-alpine3.10 as builder

WORKDIR /bb-cloud
COPY . .

RUN go build -v -o bb-cloud .


FROM alpine:3.10

RUN apk update && \
    apk add ca-certificates && \
    rm -rf /var/cache/apk/*

COPY --from=builder /bb-cloud/bb-cloud /bin/main

CMD ["/bin/main"]
