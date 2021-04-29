package net

import (
	"context"
	"crypto/x509"
	"fmt"
	"io"
	"time"

	"github.com/google/uuid"
	"github.com/pkg/errors"
	"github.com/rs/zerolog/log"
	"github.com/sushshring/torrxfer/pkg/common"
	"github.com/sushshring/torrxfer/pkg/crypto"
	pb "github.com/sushshring/torrxfer/rpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/encoding/gzip"
	_ "google.golang.org/grpc/encoding/gzip"
	"google.golang.org/grpc/metadata"
)

const transferFileOauthCredentialScope string = "transferFileOauthCredentialScope"

// TorrxferServerConnection represents a wrapper around the gRPC mechanisms to
// talk to the torrxfer server
type TorrxferServerConnection interface {
	QueryFile(file string, mediaPrefix string, correlationUUID string) (*RPCFile, error)
	TransferFile(fileBytes *io.PipeReader, blockSize uint32, offset uint64, correlationUUID string) (fileSummaryChan chan FileTransferNotification, err error)
}

type torrxferServerConnection struct {
	cc   grpc.ClientConnInterface
	uuid uuid.UUID
}

// TransferNotificationType is an iota
type TransferNotificationType uint8

const (
	// TransferNotificationTypeError Error
	TransferNotificationTypeError TransferNotificationType = iota
	// TransferNotificationTypeBytes sent bytes
	TransferNotificationTypeBytes
	// TransferNotificationTypeClosed Closed connection
	TransferNotificationTypeClosed
)

// FileTransferNotification Updated file notification
type FileTransferNotification struct {
	NotificationType TransferNotificationType
	Filepath         string
	LastTransferred  uint64
	CurrentOffset    uint64
	Error            error
}

// NewTorrxferServerConnection constructs a new server connection given server config
func NewTorrxferServerConnection(server common.ServerConnectionConfig) (TorrxferServerConnection, error) {
	if server.Address == "" {
		err := errors.New("No server address provided")
		common.LogError(err, "")
		return nil, err
	}
	address := fmt.Sprintf("%s:%d", server.Address, server.Port)
	var opts []grpc.DialOption
	if server.UseTLS {
		certPool := x509.NewCertPool()
		valid, cert, _ := crypto.VerifyCert(server.CertFile, server.Address)
		if !valid {
			log.Debug().Msg("Cert could not be validted. Continuing anyway for now")
		}
		if cert != nil {
			certPool.AddCert(cert)
		}

		creds := credentials.NewClientTLSFromCert(certPool, server.Address)
		opts = append(opts, grpc.WithTransportCredentials(creds))
		// perRPCCred, err := oauth.NewServiceAccountFromFile(server.OAuthFile, transferFileOauthCredentialScope)
		// if err != nil {
		// common.LogErrorStack(err, "Failed to initialize auth cred")
		// return nil, err
		// }
		// opts = append(opts, grpc.WithPerRPCCredentials(perRPCCred))
	} else {
		opts = append(opts, grpc.WithInsecure())
	}
	opts = append(opts, grpc.WithBlock(), grpc.WithDefaultCallOptions(grpc.UseCompressor(gzip.Name)))
	grpc.EnableTracing = true
	conn, err := grpc.Dial(address, opts...)
	if err != nil {
		log.Debug().Err(err).Msg("Could not grpc dial")
		return nil, err
	}
	log.Debug().Msg("Connected!")
	serverConnection := &torrxferServerConnection{conn, uuid.New()}
	return serverConnection, nil
}

// QueryFile makes a gRPC call to the provided server and either returns a file summary or FileNotFoundException
func (client *torrxferServerConnection) QueryFile(filePath string, mediaPrefix string, correlationUUID string) (*RPCFile, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	file, err := NewFile(filePath)
	if err := file.SetMediaPath(mediaPrefix); err != nil {
		common.LogError(err, "Could not set media prefix")
		return nil, err
	}
	ctx = metadata.AppendToOutgoingContext(ctx, "clientdata", correlationUUID)
	if err != nil {
		common.LogError(err, "Could not create file")
		return nil, err
	}
	conn := pb.NewRpcTorrxferServerClient(client.cc)
	fileSummary, err := conn.QueryFile(ctx, file.file)
	if err != nil {
		return nil, err
	}
	return NewFileFromGrpc(fileSummary), nil
}

// TransferFile makes a gRPC call to the provided server and transfer the file data as a stream
func (client *torrxferServerConnection) TransferFile(fileBytes *io.PipeReader, blockSize uint32, offset uint64, correlationUUID string) (fileSummaryChan chan FileTransferNotification, err error) {
	conn := pb.NewRpcTorrxferServerClient(client.cc)
	fileSummaryChan = make(chan FileTransferNotification)

	ctx := context.Background()
	ctx = metadata.AppendToOutgoingContext(ctx, "clientdata", correlationUUID)
	stream, err := conn.TransferFile(ctx)
	if err != nil {
		log.Debug().Err(err).Msg("Could not start transferring the file")
		return nil, err
	}
	go func(blockSize uint32, startingOffset uint64) {
		defer close(fileSummaryChan)
		defer stream.CloseAndRecv()
		defer fileBytes.Close()
		currentOffset := startingOffset
		bytes := make([]byte, blockSize)
		for {
			n, err := fileBytes.Read(bytes)
			if err != nil {
				if err == io.EOF {
					log.Trace().Msg("Finished reading")
					break
				}
				log.Debug().Err(err).Msg("Failure while reading")
				fileSummaryChan <- FileTransferNotification{
					NotificationType: TransferNotificationTypeError,
					LastTransferred:  0,
					CurrentOffset:    currentOffset,
					Error:            err,
				}
				return
			}
			log.Trace().Int("size", n).Bytes("data", bytes).Msg("Sending file bytes")
			internalErr := stream.Send(&pb.TransferFileRequest{
				Data:   bytes[:n],
				Size:   uint32(n),
				Offset: currentOffset,
			})
			if internalErr != nil {
				log.Debug().Err(err).Msg("Error transmitting file data")
				fileSummaryChan <- FileTransferNotification{
					NotificationType: TransferNotificationTypeError,
					LastTransferred:  0,
					CurrentOffset:    currentOffset,
					Error:            internalErr,
				}
				return
			}

			// Send file transmit notification
			currentOffset += uint64(n)
			fileSummaryChan <- FileTransferNotification{
				NotificationType: TransferNotificationTypeBytes,
				LastTransferred:  uint64(n),
				CurrentOffset:    currentOffset,
				Error:            nil,
			}
		}
		fileSummaryChan <- FileTransferNotification{
			NotificationType: TransferNotificationTypeClosed,
			LastTransferred:  0,
			CurrentOffset:    currentOffset,
			Error:            nil,
		}
	}(blockSize, offset)
	return
}
