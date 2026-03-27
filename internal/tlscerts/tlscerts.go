package tlscerts

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"os"
	"time"
)

func Load(certFile, keyFile string) (tls.Certificate, error) {
	return tls.LoadX509KeyPair(certFile, keyFile)
}

func SelfSigned(domains ...string) (tls.Certificate, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("generate key: %w", err)
	}

	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: domains[0]},
		DNSNames:     domains,
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("create cert: %w", err)
	}

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("marshal key: %w", err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})

	return tls.X509KeyPair(certPEM, keyPEM)
}

func LoadOrSelfSigned(certFile, keyFile string, domains ...string) (tls.Certificate, error) {
	if certFile != "" && keyFile != "" {
		if _, err := os.Stat(certFile); err == nil {
			return Load(certFile, keyFile)
		}
	}
	return SelfSigned(domains...)
}

func BuildTLSConfig(certFile, keyFile, domain string, extraDomains []struct{ Domain, CertFile, KeyFile string }) (*tls.Config, error) {
	certMap := make(map[string]*tls.Certificate)

	for _, d := range extraDomains {
		cert, err := Load(d.CertFile, d.KeyFile)
		if err != nil {
			return nil, fmt.Errorf("load cert for %s: %w", d.Domain, err)
		}
		c := cert
		certMap[d.Domain] = &c
	}

	defaultCert, err := LoadOrSelfSigned(certFile, keyFile, domain, "*."+domain)
	if err != nil {
		return nil, fmt.Errorf("load default cert: %w", err)
	}

	return &tls.Config{
		GetCertificate: func(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
			if cert, ok := certMap[hello.ServerName]; ok {
				return cert, nil
			}
			if idx := len(hello.ServerName) - len(domain) - 1; idx > 0 {
				parent := hello.ServerName[idx+1:]
				if cert, ok := certMap[parent]; ok {
					return cert, nil
				}
			}
			return &defaultCert, nil
		},
	}, nil
}
