package conf

import "github.com/sushshring/torrxfer/pkg/common"

// ServerConfig describes all the environment specific details for running the server
type ServerConfig struct {
	Debug   bool                  `envconfig:"DEBUG" default:"true"`
	Address string                `envconfig:"ADDRESS" default:"localhost"`
	Port    uint32                `envconfig:"PORT" default:"9600"`
	Logfile common.LogFileDecoder `envconfig:"LOGFILE" default:""`
}
