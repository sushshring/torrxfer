package net

import (
	"context"

	pb "github.com/sushshring/torrxfer/rpc"
)

type RpcTorrxferServer struct {
	pb.UnimplementedRpcTorrxferServerServer
}

func (RpcTorrxferServer) TransferFile(context.Context, *pb.TransferFileRequest) (*pb.FileSummary, error) {
	return nil, nil
}

func (RpcTorrxferServer) QueryFile(context.Context, *pb.File) (*pb.FileSummary, error) {
	return nil, nil
}
