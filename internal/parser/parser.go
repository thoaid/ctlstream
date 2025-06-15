package parser

import (
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"

	ct "github.com/google/certificate-transparency-go"
	"github.com/google/certificate-transparency-go/tls"
)

func ParseCertificates(leafB64, extraB64 string) ([]*x509.Certificate, error) {
	leafData, err := base64.StdEncoding.DecodeString(leafB64)
	if err != nil {
		return nil, err
	}

	var leaf ct.MerkleTreeLeaf
	if _, err := tls.Unmarshal(leafData, &leaf); err != nil {
		return nil, err
	}

	var certs []*x509.Certificate

	if leaf.TimestampedEntry.EntryType == ct.X509LogEntryType &&
		leaf.TimestampedEntry.X509Entry != nil {
		if cert, err := x509.ParseCertificate(leaf.TimestampedEntry.X509Entry.Data); err == nil {
			certs = append(certs, cert)
		}
	}

	if extraB64 != "" {
		if extraData, err := base64.StdEncoding.DecodeString(extraB64); err == nil {
			certs = append(certs, parseCertChain(extraData)...)
		}
	}

	return certs, nil
}

func parseCertChain(data []byte) []*x509.Certificate {
	var certs []*x509.Certificate

	for len(data) >= 3 {
		length := int(data[0])<<16 | int(data[1])<<8 | int(data[2])
		if len(data) < 3+length {
			break
		}

		if cert, err := x509.ParseCertificate(data[3 : 3+length]); err == nil {
			certs = append(certs, cert)
		}

		data = data[3+length:]
	}

	return certs
}

func CertToPEM(cert *x509.Certificate) string {
	return string(pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: cert.Raw,
	}))
}
