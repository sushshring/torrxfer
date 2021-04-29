#build stage
FROM --platform=linux/amd64 golang:1.16 AS protoc
RUN apt update && apt install -y protobuf-compiler
COPY ./proto /src/proto
COPY ./Makefile ./Makefile-Arch.dep /src/
# Make runs protoc-install as a step to download the file from the protoc source. Skip since we are installing from apt
RUN touch /src/protoc-install
WORKDIR /src
RUN make protoc

FROM golang:1.16 AS builder
RUN update-ca-certificates
COPY . /src
COPY --from=protoc /src/rpc /src/rpc
WORKDIR /src
RUN make

#Client builder
FROM gcr.io/distroless/base AS torrxfer-client
COPY --from=builder /src/bin/torrxfer-client /bin/torrxfer-client
LABEL Name=torrxfer-client Version=0.0.1
VOLUME [ "/config/client.json" ]
ENTRYPOINT ["/bin/torrxfer-client", "--config=/config/client.json"]

#Server builder
FROM gcr.io/distroless/base AS torrxfer-server
COPY --from=builder /src/bin/torrxfer-server /bin/torrxfer-server
LABEL Name=torrxfer-server Version=0.0.1
VOLUME ["/transfers", "/keys/cafile.pem", "/keys/keyfile.pem", "/db"]
ENV TORRXFER_SERVER_MEDIADIR=/transfers
ENV TORRXFER_SERVER_DBDIR=/db
EXPOSE 9650
ENTRYPOINT ["/bin/torrxfer-server"]