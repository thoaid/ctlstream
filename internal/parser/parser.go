package parser

import (
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"

	ct "github.com/google/certificate-transparency-go"
	"github.com/google/certificate-transparency-go/tls"
)

type DNInfo struct {
	CommonName         string   `json:"CN"`
	Organization       []string `json:"O"`
	OrganizationalUnit []string `json:"OU"`
	Country            []string `json:"C"`
	Raw                string   `json:"raw"`
}

type SANInfo struct {
	DNSNames    []string `json:"dns_names"`
	IPAddresses []string `json:"ip_addresses"`
}

type FingerprintInfo struct {
	SHA256 string `json:"sha256"`
}

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

func GetFingerprint(cert *x509.Certificate) FingerprintInfo {
	sha256Hash := sha256.Sum256(cert.Raw)
	sha256Hex := hex.EncodeToString(sha256Hash[:])

	return FingerprintInfo{
		SHA256: sha256Hex,
	}
}

func ParseDN(name pkix.Name) DNInfo {
	return DNInfo{
		CommonName:         name.CommonName,
		Organization:       name.Organization,
		OrganizationalUnit: name.OrganizationalUnit,
		Country:            name.Country,
		Raw:                name.String(),
	}
}

func ParseSANs(cert *x509.Certificate) SANInfo {
	san := SANInfo{}

	if len(cert.DNSNames) > 0 {
		san.DNSNames = cert.DNSNames
	}

	if len(cert.IPAddresses) > 0 {
		san.IPAddresses = make([]string, len(cert.IPAddresses))
		for i, ip := range cert.IPAddresses {
			san.IPAddresses[i] = ip.String()
		}
	}

	return san
}
