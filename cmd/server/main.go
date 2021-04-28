package main

import (
	glog "log"
	"os"

	"github.com/alecthomas/kingpin"
	"github.com/kelseyhightower/envconfig"
	"github.com/rs/zerolog"
	"github.com/sushshring/torrxfer/pkg/common"
	"github.com/sushshring/torrxfer/pkg/server"
)

var (
	app     = kingpin.New("torrxfer-server", "Torrent downloaded file transfer server")
	debug   = app.Flag("debug", "Enable debug mode").Default("false").OverrideDefaultFromEnvar("TORRXFER_SERVER_DEBUG").Bool()
	tls     = app.Flag("tls", "Should server use TLS vs plain TCP").Default("false").OverrideDefaultFromEnvar("TORRXFER_SERVER_TLS").Bool()
	cafile  = app.Flag("cafile", "The file containing the CA root cert file").String()
	keyfile = app.Flag("keyfile", "The file containing the CA root key file").String()
	trace   = app.Flag("trace", "Enable trace mode").Default("false").OverrideDefaultFromEnvar("TORRXFER_SERVER_TRACE").Bool()
	version = "0.1"
)

func main() {
	app.Version(version)
	kingpin.MustParse(app.Parse(os.Args[1:]))
	var serverConf common.ServerConfig
	err := envconfig.Process("TORRXFER_SERVER", &serverConf)
	if err != nil {
		glog.Fatal(err)
	}
	// Configure logging
	var level zerolog.Level
	if *trace {
		level = zerolog.TraceLevel
	} else if *debug {
		level = zerolog.DebugLevel
	} else {
		level = zerolog.InfoLevel
	}
	common.ConfigureLogging(level, false, serverConf.Logfile.Writer, os.Stdout)
	server.RunServer(serverConf, *tls, *cafile, *keyfile)
}
