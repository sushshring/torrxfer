package net

import (
	"context"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/sushshring/torrxfer/rpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
)

func addFileMetadataToContext(ctx context.Context, file *rpc.File) (retCtx context.Context) {
	md := metadata.New(nil)
	md.Append("filedata", file.DataHash, file.Name, file.MediaDirectory)
	retCtx = metadata.NewOutgoingContext(ctx, md)
	return
}

func getFileMetadataFromContext(ctx context.Context) (file *rpc.File, code codes.Code) {
	file = nil
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		log.Debug().Msg("Failed to parse metadata")
		code = codes.InvalidArgument
		return
	}
	data := md.Get("filedata")
	file = &rpc.File{
		Name:           data[1],
		DataHash:       data[0],
		MediaDirectory: data[2],
		CreatedTime:    uint64(time.Unix(0, 0).Unix()),
		ModifiedTime:   uint64(time.Unix(0, 0).Unix()),
		Size:           uint64(time.Unix(0, 0).Unix()),
	}
	code = codes.OK
	return
}
