package net

import (
	"context"
	"crypto/x509"

	"github.com/pkg/errors"
	"github.com/rs/zerolog/log"
	"github.com/sushshring/torrxfer/pkg/common"
	pb "github.com/sushshring/torrxfer/rpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

// TorrxferServerConnection represents a wrapper around the gRPC mechanisms to
// talk to the torrxfer server
type TorrxferServerConnection struct {
	cc grpc.ClientConnInterface
}

// NewTorrxferServerConnection constructs a new server connection given server config
func NewTorrxferServerConnection(server *common.ServerConnectionConfig) (*TorrxferServerConnection, error) {
	if server.Address == "" {
		err := errors.New("No server address provided")
		common.LogError(err, "")
		return nil, err
	}
	var opts []grpc.DialOption
	if server.UseTLS {
		certPool := x509.NewCertPool()
		certPool.AddCert(server.CertFile)

		creds := credentials.NewClientTLSFromCert(certPool, server.Address)
		opts = append(opts, grpc.WithTransportCredentials(creds))
	} else {
		opts = append(opts, grpc.WithInsecure())
	}
	conn, err := grpc.Dial(server.Address, opts...)
	if err != nil {
		common.LogError(err, "Could not grpc dial")
		return nil, nil
	}
	log.Debug().Msg("Connected!")
	serverConnection := new(TorrxferServerConnection)
	serverConnection.cc = conn
	return serverConnection, nil
}

// QueryFile makes an gRPC call to the provided server and either returns a file summary or FileNotFoundException
func (client *TorrxferServerConnection) QueryFile(filePath string) (*pb.FileSummary, error) {
	file, err := NewFile(filePath)
	if err != nil {
		common.LogError(err, "Could not create file")
		return nil, err
	}
	conn := pb.NewRpcTorrxferServerClient(client.cc)
	return conn.QueryFile(context.Background(), file.file)
}
