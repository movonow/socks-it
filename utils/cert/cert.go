package cert

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"log"
	"math/big"
	"net"
	"os"
	"strings"
	"time"
)

func GenerateCA(subject *pkix.Name, path string) (*x509.Certificate, *rsa.PrivateKey, error) {
	// creating a CA which will be used to sign all of our certificates using the x509 package from the Go Standard Library
	caCert := &x509.Certificate{
		SerialNumber:          big.NewInt(2019),
		Subject:               *subject,
		NotBefore:             time.Now(),
		NotAfter:              time.Now().AddDate(10*365, 0, 0),
		IsCA:                  true, // <- indicating this certificate is a CA certificate.
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth},
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
	}
	// generate a private key for the CA
	caKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		log.Printf("Generate the CA Private Key error: %v\n", err)
		return nil, nil, err
	}

	// create the CA certificate
	caBytes, err := x509.CreateCertificate(rand.Reader, caCert, caCert, &caKey.PublicKey, caKey)
	if err != nil {
		log.Printf("Create the CA Certificate error: %v\n", err)
		return nil, nil, err
	}

	// Create the CA PEM files
	caPEM := new(bytes.Buffer)
	err = pem.Encode(caPEM, &pem.Block{
		Type:  "CERTIFICATE",
		Bytes: caBytes,
	})
	if err != nil {
		return nil, nil, err
	}

	if err := os.WriteFile(path+"ca.crt", caPEM.Bytes(), 0644); err != nil {
		log.Printf("Write the CA certificate file error: %v\n", err)
		return nil, nil, err
	}

	//caPrivateKeyPEM := new(bytes.Buffer)
	//err = pem.Encode(caPrivateKeyPEM, &pem.Block{
	//	Type:  "RSA PRIVATE KEY",
	//	Bytes: x509.MarshalPKCS1PrivateKey(caKey),
	//})
	//if err != nil {
	//	return nil, nil, err
	//}
	//if err = os.WriteFile(path+"ca.key", caPEM.Bytes(), 0644); err != nil {
	//	log.Printf("Write the CA certificate file error: %v\n", err)
	//	return nil, nil, err
	//}

	return caCert, caKey, nil
}

func GenerateCert(caCert *x509.Certificate, caKey *rsa.PrivateKey, subject *pkix.Name, addresses []net.IP, outputDir string) error {
	cert := &x509.Certificate{
		SerialNumber: big.NewInt(1658),
		Subject:      *subject,
		IPAddresses:  addresses,
		//DNSNames:     []string{"localhost"},
		NotBefore:    time.Now(),
		NotAfter:     time.Now().AddDate(10, 0, 0),
		SubjectKeyId: []byte{1, 2, 3, 4, 6},
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth},
		KeyUsage:     x509.KeyUsageDigitalSignature,
	}

	commonName := strings.Split(subject.CommonName, ".")[0]
	commonName = strings.Replace(commonName, " ", "-", -1)
	commonName = strings.ToLower(commonName)

	certKey, err := rsa.GenerateKey(rand.Reader, 4096)
	if err != nil {
		log.Printf("Generate the Key error: %v\n", err)
		return err
	}
	certBytes, err := x509.CreateCertificate(rand.Reader, cert, caCert, &certKey.PublicKey, caKey)
	if err != nil {
		log.Printf("Generate the certificate error: %v\n", err)
		return err
	}

	certPEM := new(bytes.Buffer)
	err = pem.Encode(certPEM, &pem.Block{
		Type:  "CERTIFICATE",
		Bytes: certBytes,
	})
	if err != nil {
		return err
	}
	if err = os.WriteFile(outputDir+commonName+".crt", certPEM.Bytes(), 0644); err != nil {
		log.Printf("Write the CA certificate file error: %v\n", err)
		return err
	}

	certKeyPEM := new(bytes.Buffer)
	err = pem.Encode(certKeyPEM, &pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(certKey),
	})
	if err != nil {
		return err
	}
	if err = os.WriteFile(outputDir+commonName+".key", certKeyPEM.Bytes(), 0644); err != nil {
		log.Printf("Write the CA certificate file error: %v\n", err)
		return err
	}
	return nil
}
