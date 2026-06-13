package tls

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"os"

	"google.golang.org/grpc/credentials"
)

// NewServerCredentials builds gRPC transport credentials for the sidecar server.
// Enforces mutual TLS — any client that cannot present a certificate signed by
// the CA is rejected at the TLS handshake before any RPC is processed.
func NewServerCredentials(caCertPath, serverCertPath, serverKeyPath string) (credentials.TransportCredentials, error) {
	// Load the sidecar's own certificate and key.
	serverCert, err := tls.LoadX509KeyPair(serverCertPath, serverKeyPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load server cert/key pair: %w", err)
	}

	// Load the CA certificate to verify client certificates.
	caPool, err := loadCACert(caCertPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load CA cert: %w", err)
	}

	tlsCfg := &tls.Config{
		// Present sidecar certificate to the client.
		Certificates: []tls.Certificate{serverCert},

		// Require and verify the client's certificate.
		// RequireAndVerifyClientCert means the handshake fails if the client
		// presents no cert or a cert not signed by the CA.
		ClientAuth: tls.RequireAndVerifyClientCert,
		ClientCAs:  caPool,

		// Minimum TLS 1.3 doesn't allow lower versions to work
		MinVersion: tls.VersionTLS13,
	}

	return credentials.NewTLS(tlsCfg), nil
}

// NewClientCredentials builds gRPC transport credentials for the validator (client) side.
// The validator presents its client certificate to the sidecar for authentication.
func NewClientCredentials(caCertPath, clientCertPath, clientKeyPath, serverName string) (credentials.TransportCredentials, error) {
	// Load the validator's client certificate and key.
	clientCert, err := tls.LoadX509KeyPair(clientCertPath, clientKeyPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load client cert/key pair: %w", err)
	}

	// Load the CA certificate to verify the sidecar's server certificate.
	caPool, err := loadCACert(caCertPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load CA cert: %w", err)
	}

	if serverName == "" {
		return nil, errors.New("serverName must not be empty and must match CN in sidecar's server cert")
	}

	tlsCfg := &tls.Config{
		// Share the client certificate to the sidecar.
		Certificates: []tls.Certificate{clientCert},

		// Verify the sidecar's certificate against the CA.
		RootCAs:    caPool,
		ServerName: serverName,
		MinVersion: tls.VersionTLS13,
	}

	return credentials.NewTLS(tlsCfg), nil
}

// loadCACert reads a PEM-encoded CA certificate file and returns a cert pool.
func loadCACert(path string) (*x509.CertPool, error) {
	caPEM, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("cannot read CA cert file %q: %w", path, err)
	}

	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caPEM) {
		return nil, fmt.Errorf("no valid certificates found in %q", path)
	}

	return pool, nil
}
