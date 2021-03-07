package common

import "crypto/x509"

const (
	DefaultAddress string = "localhost"
	DefaultPort    uint64 = 9650
)

type ServerConnectionConfig struct {
	Address  string
	Port     uint32
	UseTLS   bool
	CertFile *x509.Certificate
}
