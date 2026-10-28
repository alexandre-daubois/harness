package modelproxy

import (
	"crypto/x509"
	"testing"
)

func TestNewCertificateIsOnlyTrustedForTheModelProxyHost(t *testing.T) {
	caPEM, cert, err := NewCertificate("host.docker.internal")
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(caPEM) {
		t.Fatal("the CA is not valid PEM")
	}
	leaf, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		t.Fatal(err)
	}

	if _, err := leaf.Verify(x509.VerifyOptions{Roots: roots, DNSName: "host.docker.internal"}); err != nil {
		t.Errorf("the container would reject the model proxy: %v", err)
	}
	if _, err := leaf.Verify(x509.VerifyOptions{Roots: roots, DNSName: "chatgpt.com"}); err == nil {
		t.Error("the certificate is valid for another host")
	}
	if leaf.IsCA {
		t.Error("the server certificate can sign certificates")
	}
}

func TestNewCertificateIsOnlyTrustedForTheModelProxyIP(t *testing.T) {
	caPEM, cert, err := NewCertificate("192.0.2.1")
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(caPEM) {
		t.Fatal("the CA is not valid PEM")
	}
	leaf, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		t.Fatal(err)
	}

	if _, err := leaf.Verify(x509.VerifyOptions{Roots: roots, DNSName: "192.0.2.1"}); err != nil {
		t.Errorf("a container reaching the host by gateway IP would reject the model proxy: %v", err)
	}
	if _, err := leaf.Verify(x509.VerifyOptions{Roots: roots, DNSName: "192.0.2.2"}); err == nil {
		t.Error("the certificate is valid for another address")
	}
}
