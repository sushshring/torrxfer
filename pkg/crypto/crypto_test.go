package crypto

import (
	"bytes"
	"crypto/x509"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

func TestFileHash(t *testing.T) {
	const (
		hashString string = "Hello file"
		knownHash  string = "46514f703f6067c6ab40de1948e761f669c741e95f3d6dca566779a638f25340"
		filename   string = "testfile"
	)
	file, err := os.Create(filepath.Join(".", filename))
	if err != nil {
		t.Error(err)
	}
	if _, err := file.WriteString(hashString); err != nil {
		t.Error(err)
	}
	hash, err := HashFile(filepath.Join(".", filename))
	if err != nil {
		t.Error(err)
	}
	if hash != knownHash {
		t.Errorf("Hash was incorrect. Got %s, expected %s", hash, knownHash)
	}
}

func TestStringHash(t *testing.T) {
	const (
		hashString string = "Hello file"
		knownHash  string = "46514f703f6067c6ab40de1948e761f669c741e95f3d6dca566779a638f25340"
	)

	hash, err := Hash(hashString)
	if err != nil {
		t.Error(err)
	}
	if hash != knownHash {
		t.Errorf("Hash was incorrect. Got %s, expected %s", hash, knownHash)
	}
}

func TestVerifyCert(t *testing.T) {
	const (
		website  string = "www.google.com"
		certFile string = "google.pem"
	)
	var (
		certData *x509.Certificate
	)
	// Get certificate for known website
	resp, err := http.Head(fmt.Sprintf("https://%s", website))
	if err != nil {
		t.Error(err)
		return
	}
	certData = resp.TLS.PeerCertificates[0]

	valid, cert, err := VerifyCert(certFile, website)
	if err != nil {
		t.Error(err)
		return
	}
	if !valid {
		t.Errorf("Cert not valid")
		return
	}
	if !bytes.Equal(cert.Raw, certData.Raw) {
		t.Log("Cert data does not match retrieved")
		return
	}
}
