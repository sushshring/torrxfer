package main

import (
	"fmt"
	glog "log"
	gnet "net"
	"os"

	"github.com/alecthomas/kingpin"
	"github.com/kelseyhightower/envconfig"
	"github.com/rs/zerolog/log"
	"github.com/sushshring/torrxfer/pkg/conf"
	"github.com/sushshring/torrxfer/pkg/net"
	pb "github.com/sushshring/torrxfer/rpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

var (
	app     = kingpin.New("torrxfer-server", "Torrent downloaded file transfer server")
	debug   = app.Flag("debug", "Enable debug mode").Default("false").Bool()
	tls     = app.Flag("tls", "Should server use TLS vs plain TCP").Default("false").OverrideDefaultFromEnvar("TORRXFER_SERVER_TLS").Bool()
	cafile  = app.Flag("cafile", "The file containing the CA root cert file").String()
	keyfile = app.Flag("cafile", "The file containing the CA root key file").String()
	version = "0.1"
)

func main() {
	kingpin.Version(version)
	kingpin.Parse()
	var serverConf conf.ServerConfig
	err := envconfig.Process("TORRXFER_SERVER", &serverConf)
	if err != nil {
		glog.Fatal(err)
	}
	// Configure logging
	conf.ConfigureLogging(*debug, serverConf.Logfile.Writer)
	runServer(serverConf)
}

func runServer(serverConf conf.ServerConfig) {
	lis, err := gnet.Listen("tcp", fmt.Sprintf("localhost:%d", serverConf.Port))
	if err != nil {
		log.Fatal().Err(err).Msg("Could not start server")
	}
	var opts []grpc.ServerOption
	if *tls {
		if *cafile == "" {
			log.Fatal().Msg("CA File must be provided to run with TLS")
		}
		if _, err := os.Stat(*cafile); os.IsNotExist(err) {
			log.Fatal().Msg("Valid CA file must be provided to run with TLS")
		}
		if *keyfile == "" {
			log.Fatal().Msg("CA File must be provided to run with TLS")
		}
		if _, err := os.Stat(*keyfile); os.IsNotExist(err) {
			log.Fatal().Msg("Valid CA file must be provided to run with TLS")
		}
		creds, err := credentials.NewServerTLSFromFile(*cafile, *keyfile)
		if err != nil {
			log.Fatal().Err(err).Msg("Failed to generate credentials")
		}
		opts = []grpc.ServerOption{grpc.Creds(creds)}
	}
	grpcServer := grpc.NewServer(opts...)
	server := &net.RpcTorrxferServer{}
	pb.RegisterRpcTorrxferServerServer(grpcServer, server)
	grpcServer.Serve(lis)
}
