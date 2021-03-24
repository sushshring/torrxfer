package common

const DefaultBlockSize = 1024

// ServerConfig describes all the environment specific details for running the server
type ServerConfig struct {
	Debug   bool             `envconfig:"DEBUG" default:"true"`
	Address string           `envconfig:"ADDRESS" default:"localhost" json:"Address"`
	Port    uint32           `envconfig:"PORT" default:"9650" json:"Port"`
	Logfile LogFileDecoder   `envconfig:"LOGFILE" default:""`
	SaveDir DirectoryDecoder `envconfig:"MEDIADIR" default:"."`
}
