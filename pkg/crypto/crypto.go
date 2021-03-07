package crypto

import (
	"crypto/sha256"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"io"
	"os"

	"github.com/pkg/errors"
	"github.com/sushshring/torrxfer/pkg/common"
)

// HashFile calculates the SHA256 hash of the current state of the file
func HashFile(filepath string) (string, error) {
	input, err := os.Open(filepath)
	if err != nil {
		common.LogError(err, "Could not open hashfile")
		return "", err
	}
	hash := sha256.New()
	if _, err := io.Copy(hash, input); err != nil {
		common.LogError(err, "Could not copy file contexts")
		return "", err
	}
	sum := hash.Sum(nil)
	return fmt.Sprintf("%x", sum), nil
}

// VerifyCert verifies is a pem cert file is valid and trusted
func VerifyCert(filepath string, hostname string) (bool, *x509.Certificate, error) {
	bytes, err := os.ReadFile(filepath)
	if err != nil {
		common.LogError(err, "Could not open cert file")
		return false, nil, err
	}
	roots := x509.NewCertPool()

	block, _ := pem.Decode([]byte(bytes))
	if block == nil {
		common.LogError(errors.New("Failed to parse certificate PEM"), "")
		return false, nil, err
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		common.LogError(err, "Failed to parse certificate")
		return false, nil, err
	}

	opts := x509.VerifyOptions{
		DNSName: hostname,
		Roots:   roots,
	}

	if _, err := cert.Verify(opts); err != nil {
		common.LogError(err, "Failed to verify certificate")
		return false, nil, err
	}
	return true, cert, nil
}
