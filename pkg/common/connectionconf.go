package common

import "crypto/x509"

const (
	DefaultAddress string = "localhost"
	DefaultPort    uint64 = 9650
)

type WatchedDirectory struct {
	Directory string `json:"Directory"`
	MediaRoot string `json:"MediaRoot"`
}

type ClientConfig struct {
	Servers            []ServerConnectionConfig `json:"Servers"`
	WatchedDirectories []WatchedDirectory       `json:"WatchedDirectories"`
}

type ServerConnectionConfig struct {
	Address  string            `json:"Address"`
	Port     uint32            `json:"Port"`
	UseTLS   bool              `json:"Secure"`
	CertFile *x509.Certificate `json:"CertFile"`
}
