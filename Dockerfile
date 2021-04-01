#build stage
FROM golang:alpine AS builder
RUN apk add --no-cache git alpine-sdk
COPY . /src
WORKDIR /src
RUN make vendor
RUN make

#Client builder
FROM alpine:latest AS torrxfer-client
RUN apk --no-cache add ca-certificates
COPY --from=builder /src/bin/torrxfer-client /bin/torrxfer-client
LABEL Name=torrxfer-client Version=0.0.1
VOLUME [ "/config/client.json" ]
ENTRYPOINT ["/bin/torrxfer-client", "--config=/config/client.json"]

#Server builder
FROM alpine:latest AS torrxfer-server
RUN apk --no-cache add ca-certificates
COPY --from=builder /src/bin/torrxfer-server /bin/torrxfer-server
LABEL Name=torrxfer-server Version=0.0.1
VOLUME ["/transfers", "/keys/cafile.pem", "/keys/keyfile.pem"]
ENV TORRXFER_SERVER_MEDIADIR=/transfers
EXPOSE 9650
ENTRYPOINT ["/bin/torrxfer-server"]