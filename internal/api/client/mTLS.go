/* mTLS.go

mTLS.go loads relevant certifications and specifies the TLS configuration for
the client-side mTLS authentication protocol for the remote job execution service
*/

package client

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"

	"google.golang.org/grpc/credentials"
)

// ConfigureClientTLS creates TLS credentials for client authentication
func ConfigureClientTLS(certFile string, keyFile string, caFile string) (credentials.TransportCredentials, error) {
	// Load CA cert to verify server
	caCert, err := os.ReadFile(caFile)
	if err != nil {
		return nil, fmt.Errorf("failed to load CA certificate: %v", err)
	}

	// Create cert pool that validates servers this client connects to
	certPool := x509.NewCertPool()
	if !certPool.AppendCertsFromPEM(caCert) {
		return nil, fmt.Errorf("failed to add CA certificate to pool")
	}

	// Load client cert/key pair for client authentication
	clientCert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, fmt.Errorf("failed to load client cert/key pair: %v", err)
	}

	// Configure TLS
	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{clientCert}, // client authentication
		RootCAs:      certPool,                      // trust servers signed by CA
		MinVersion:   tls.VersionTLS13,              // enforce TLS v1.3
	}

	return credentials.NewTLS(tlsConfig), nil
}
