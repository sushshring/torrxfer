package net

import (
	"context"
	"errors"
	"io"

	"github.com/rs/zerolog/log"
	"github.com/sushshring/torrxfer/pkg/common"
	pb "github.com/sushshring/torrxfer/rpc"
	"google.golang.org/grpc/metadata"
)

type ITorrxferServer interface {
	QueryFunction(clientID string, file *RPCFile) (*RPCFile, error)
	TransferFunction(clientID string, fileBytes []byte, blockSize uint32, currentOffset uint64) error
	RegisterForWriteNotification(clientID string) (chan error, chan struct{})
	Close(clientID string)
}

type RpcTorrxferServer struct {
	pb.UnimplementedRpcTorrxferServerServer
	server ITorrxferServer
}

func NewRpcTorrxferServer(torrxferServer ITorrxferServer) (server *RpcTorrxferServer) {
	server = &RpcTorrxferServer{
		UnimplementedRpcTorrxferServerServer: pb.UnimplementedRpcTorrxferServerServer{},
		server:                               torrxferServer,
	}
	return
}

func (*RpcTorrxferServer) validateIncomingRequest(ctx context.Context) (clientID string, err error) {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		err := errors.New("Failed to get file metadata. Invalid argument")
		log.Debug().Err(err).Msg("")
		return "", err
	}
	clientIds, ok := md["clientdata"]
	if !ok {
		err := errors.New("Client data not provided in request")
		log.Debug().Err(err).Msg("")
		return "", err
	}
	clientID = clientIds[0]
	err = nil
	return
}

func (s *RpcTorrxferServer) TransferFile(stream pb.RpcTorrxferServer_TransferFileServer) error {
	clientID, err := s.validateIncomingRequest(stream.Context())
	if err != nil {
		return err
	}
	errorChan, doneChan := s.server.RegisterForWriteNotification(clientID)
	for {
		fileReq, err := stream.Recv()
		if err == io.EOF {
			// Finished receiving file
			log.Debug().Msg("File finished")
			s.server.Close(clientID)
			return nil
		} else if err != nil {
			common.LogErrorStack(err, "Error receiving transfer request")
			return err
		}
		log.Debug().Bytes("File data", fileReq.Data).Str("Client ID", clientID).Msg("Received transfer file data")
		err = s.server.TransferFunction(clientID, fileReq.GetData(), fileReq.GetSize(), fileReq.GetOffset())
		if err != nil {
			common.LogErrorStack(err, "Failed to write file data")
			return err
		}

		select {
		case err := <-errorChan:
			log.Debug().Err(err).Msg("Error while writing")
			return err
		case _ = <-doneChan:
			log.Debug().Msg("File transfer finished")
			return nil
		default:
			// no-op
		}
	}
}

func (s *RpcTorrxferServer) QueryFile(ctx context.Context, file *pb.File) (*pb.File, error) {
	log.Debug().Str("File name", file.Name).Msg("Received file transfer request")
	clientID, err := s.validateIncomingRequest(ctx)
	if err != nil {
		return nil, err
	}
	rpcFile := NewFileFromGrpc(file)
	rpcFile, err = s.server.QueryFunction(clientID, rpcFile)
	if err != nil {
		log.Debug().Err(err).Msg("Server query failed")
		return nil, err
	}
	return rpcFile.file, nil
}
