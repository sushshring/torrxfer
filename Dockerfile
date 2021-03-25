#build stage
FROM golang:alpine AS builder
RUN apk add --no-cache git alpine-sdk
WORKDIR /src
COPY . /src
RUN make vendor
RUN make

#Client builder
FROM alpine:latest AS torrxfer-client
RUN apk --no-cache add ca-certificates
COPY --from=builder /src/bin/torrxfer-client /bin/torrxfer-client
ENTRYPOINT ["/bin/torrxfer-client" "--config=/config/client.json"]
LABEL Name=torrxfer-client Version=0.0.1
VOLUME [ "/config/client.json" ]

#Server builder
FROM alpine:latest AS torrxfer-server
RUN apk --no-cache add ca-certificates
COPY --from=builder /src/bin/torrxfer-server /bin/torrxfer-server
ENTRYPOINT [/bin/torrxfer-server]
LABEL Name=torrxfer-server Version=0.0.1
EXPOSE 9650