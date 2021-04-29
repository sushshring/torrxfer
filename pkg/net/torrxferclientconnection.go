package net

import (
	"context"
	"errors"
	"io"

	"github.com/rs/zerolog/log"
	"github.com/sushshring/torrxfer/pkg/common"
	pb "github.com/sushshring/torrxfer/rpc"
	"google.golang.org/api/oauth2/v2"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

var (
	errMissingMetadata = status.Errorf(codes.InvalidArgument, "missing metadata")
	errInvalidToken    = status.Errorf(codes.Unauthenticated, "invalid token")
	errTransferRequest = status.Errorf(codes.Internal, "internal error on transfer")
	errQueryRequest    = status.Errorf(codes.Internal, "internal error on query")
)

// ITorrxferServer Server interface representation for client
type ITorrxferServer interface {
	QueryFunction(clientID string, file *RPCFile) (*RPCFile, error)
	TransferFunction(clientID string, fileBytes []byte, blockSize uint32, currentOffset uint64) error
	RegisterForWriteNotification(clientID string) (chan error, chan struct{})
	Close(clientID string)
}

// RPCTorrxferServer wrapper around grpc server
type RPCTorrxferServer struct {
	pb.UnimplementedRpcTorrxferServerServer
	server ITorrxferServer
}

// NewRPCTorrxferServer creates a new torrxfer rpc server
func NewRPCTorrxferServer(torrxferServer ITorrxferServer) (server *RPCTorrxferServer) {
	server = &RPCTorrxferServer{
		UnimplementedRpcTorrxferServerServer: pb.UnimplementedRpcTorrxferServerServer{},
		server:                               torrxferServer,
	}
	return
}

// func EnsureValidTokenStream(srv interface{}, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
// 	log.Debug().Msg("Validating token for stream")
// 	md, ok := metadata.FromIncomingContext(ss.Context())
// 	if !ok {
// 		return errMissingMetadata
// 	}
// 	if !validateTokenFromMetadata(md) {
// 		return errInvalidToken
// 	}
// 	return handler(srv, ss)
// }

// func EnsureValidToken(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
// 	log.Debug().Msg("Validating token for unary src")
// 	md, ok := metadata.FromIncomingContext(ctx)
// 	if !ok {
// 		return nil, errMissingMetadata
// 	}
// 	if !validateTokenFromMetadata(md) {
// 		return nil, errInvalidToken
// 	}
// 	// Continue execution of handler after ensuring a valid token.
// 	return handler(ctx, req)
// }

func validateTokenFromMetadata(md metadata.MD) bool {
	// The keys within metadata.MD are normalized to lowercase.
	// See: https://godoc.org/google.golang.org/grpc/metadata#New
	f, ok := md["authorization"]
	if !ok {
		return false
	}
	return validateToken(f)
}

func validateToken(authorization []string) bool {
	if len(authorization) < 1 {
		return false
	}
	log.Debug().Str("auth token", authorization[0]).Msg("Validating token")
	oauth2Service, err := oauth2.NewService(context.Background())
	if err != nil {
		common.LogErrorStack(err, "Could not create oauth service")
		return false
	}
	tokenInfoCall := oauth2Service.Tokeninfo()
	tokenInfoCall.AccessToken(authorization[0])
	tokenInfo, err := tokenInfoCall.Do()
	if err != nil {
		common.LogErrorStack(err, "Could not validate token")
		return false
	}
	log.Info().Str("Granted scope", tokenInfo.Scope).Str("Client", tokenInfo.UserId).Msg("Validated request")
	return true
}

func (s *RPCTorrxferServer) validateIncomingRequest(ctx context.Context) (clientID string, err error) {
	log.Debug().Msg("Validating incoming request")
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		err := errors.New("failed to get file metadata. Invalid argument")
		log.Debug().Err(err).Msg("")
		return "", errMissingMetadata
	}
	clientIds, ok := md["clientdata"]
	if !ok {
		err := errors.New("client data not provided in request")
		log.Debug().Err(err).Msg("")
		return "", errMissingMetadata
	}
	clientID = clientIds[0]
	err = nil
	log.Debug().Str("Client ID", clientID).Msg("Processing request")

	// log.Debug().Msg("Validating client authorization")
	// if !validateTokenFromMetadata(md) {
	// 	return "", errInvalidToken
	// }
	return
}

// TransferFile wrapper around gRPC TransferFile. Called by gRPC, should not be called directly
func (s *RPCTorrxferServer) TransferFile(stream pb.RpcTorrxferServer_TransferFileServer) error {
	clientID, err := s.validateIncomingRequest(stream.Context())
	if err != nil {
		return err
	}
	defer s.server.Close(clientID)
	errorChan, doneChan := s.server.RegisterForWriteNotification(clientID)
	for {
		fileReq, err := stream.Recv()
		if err == io.EOF {
			// Finished receiving file
			log.Debug().Msg("File finished")
			return nil
		} else if err != nil {
			common.LogErrorStack(err, "Error receiving transfer request")
			return errTransferRequest
		}
		log.Trace().Bytes("File data", fileReq.Data).Str("Client ID", clientID).Msg("Received transfer file data")
		err = s.server.TransferFunction(clientID, fileReq.GetData(), fileReq.GetSize(), fileReq.GetOffset())
		if err != nil {
			common.LogErrorStack(err, "Failed to write file data")
			return errTransferRequest
		}

		select {
		case err := <-errorChan:
			log.Info().Err(err).Msg("Error while writing")
			return errTransferRequest
		case <-doneChan:
			log.Info().Msg("File transfer finished")
			return nil
		default:
			// no-op
		}
	}
}

// QueryFile wrapper around gRPC query file. Called by gRPC, should not be called directly
func (s *RPCTorrxferServer) QueryFile(ctx context.Context, file *pb.File) (*pb.File, error) {
	log.Info().Str("File name", file.Name).Msg("Received file transfer request")
	clientID, err := s.validateIncomingRequest(ctx)
	if err != nil {
		return nil, errQueryRequest
	}
	rpcFile := NewFileFromGrpc(file)
	rpcFile, err = s.server.QueryFunction(clientID, rpcFile)
	if err != nil {
		log.Debug().Err(err).Msg("Server query failed")
		return nil, errQueryRequest
	}
	return rpcFile.file, nil
}
