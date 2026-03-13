package server

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"

	"google.golang.org/grpc/credentials"
)

func ConfigureServerTLS(caPath, certPath, keyPath string) (credentials.TransportCredentials, error) {
	// Load CA cert that signed client certs
	clientCA, err := os.ReadFile(caPath)
	if err != nil {
		return nil, fmt.Errorf("loading CA certification: %v", err)
	}

	// Create a cert pool and add the CA cert.
	// Any client cert signed by this CA is to be trusted.
	certPool := x509.NewCertPool()
	if !certPool.AppendCertsFromPEM(clientCA) {
		return nil, fmt.Errorf("failed to parse CA certificate")
	}

	// Load server cert and private key
	serverCert, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		return nil, fmt.Errorf("loading server x509 key pair: %v", err)
	}

	// Configure TLS
	config := &tls.Config{
		Certificates: []tls.Certificate{serverCert},  // server ID
		ClientAuth:   tls.RequireAndVerifyClientCert, // required client cert
		ClientCAs:    certPool,                       //trust clients signed by CA
		MinVersion:   tls.VersionTLS13,               // enforces TLS v1.3
	}

	return credentials.NewTLS(config), nil
}
