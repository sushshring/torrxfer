package common

import "crypto/x509"

const (
	// DefaultAddress for a server
	DefaultAddress string = "localhost"
	// DefaultPort for a server
	DefaultPort uint64 = 9650
)

// WatchedDirectory json representation
type WatchedDirectory struct {
	Directory string `json:"Directory"`
	MediaRoot string `json:"MediaRoot"`
}

// ClientConfig json representation
type ClientConfig struct {
	Servers            []ServerConnectionConfig `json:"Servers"`
	WatchedDirectories []WatchedDirectory       `json:"WatchedDirectories"`
	DeleteOnComplete   bool                     `json:"DeleteFileOnComplete"`
}

// ServerConnectionConfig json representation
type ServerConnectionConfig struct {
	Address  string            `json:"Address"`
	Port     uint32            `json:"Port"`
	UseTLS   bool              `json:"Secure"`
	CertFile *x509.Certificate `json:"CertFile"`
}
