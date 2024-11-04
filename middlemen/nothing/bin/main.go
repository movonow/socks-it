package main

import (
	"crypto/x509/pkix"
	"flag"
	"log"
	"net"
	"os"
	nothing "socks.it/nothing/internal"
	"socks.it/utils/cert"
	"strings"
)

var outputDir = flag.String("outputDir", "certs/", "output directory for certs")
var clientAddress = flag.String("clientAddress", "", "client IP address")
var serverAddress = flag.String("serverAddress", "", "server IP address")

func main() {
	flag.Parse()

	if !strings.HasSuffix(*outputDir, "/") {
		*outputDir += "/"
	}
	if _, err := os.Stat(*outputDir); os.IsNotExist(err) {
		if err = os.MkdirAll(*outputDir, 0755); err != nil {
			log.Fatalf("create %q failed: %v", *outputDir, err)
		}
	}

	serverAddresses := []net.IP{net.IPv4(127, 0, 0, 1), net.IPv6loopback}
	if *serverAddress != "" {
		address := net.ParseIP(*serverAddress)
		if address == nil {
			panic("invalid server IP address")
		}
		serverAddresses = append(serverAddresses, address)
	}

	clientAddresses := []net.IP{net.IPv4(127, 0, 0, 1), net.IPv6loopback}
	if *clientAddress != "" {
		address := net.ParseIP(*clientAddress)
		if address == nil {
			panic("invalid client IP address")
		}
		clientAddresses = append(clientAddresses, address)
		serverAddresses = append(serverAddresses, address)
	}

	generateSuite(clientAddresses, serverAddresses, *outputDir)
}

func generateSuite(clientAddresses, serverAddresses []net.IP, outputDir string) {
	subject := pkix.Name{ /**/
		Country:            []string{"Earth"},
		Organization:       []string{"CA Company"},
		OrganizationalUnit: []string{"Engineering"},
		Locality:           []string{"Mountain"},
		Province:           []string{"Asia"},
		StreetAddress:      []string{"Bridge"},
		PostalCode:         []string{"123456"},
		SerialNumber:       "",
		CommonName:         "CA",
		Names:              []pkix.AttributeTypeAndValue{},
		ExtraNames:         []pkix.AttributeTypeAndValue{},
	}
	caCert, caKey, err := cert.GenerateCA(&subject, outputDir)
	if err != nil {
		log.Fatalf("Generate CA Certificate error!")
	}
	log.Println("Create the CA certificate successfully.")

	subject.CommonName = nothing.ServerSubjectName
	subject.Organization = []string{"Server Company"}
	if err := cert.GenerateCert(caCert, caKey, &subject, serverAddresses, outputDir); err != nil {
		log.Fatal("Generate Server Certificate error!")
	}
	log.Printf("Create and Sign the %v certificate successfully.\n", nothing.ServerSubjectName)

	subject.CommonName = nothing.ClientSubjectName
	subject.Organization = []string{"Client Company"}
	if err := cert.GenerateCert(caCert, caKey, &subject, clientAddresses, outputDir); err != nil {
		log.Fatal("Generate Client Certificate error!")
	}
	log.Printf("Create and Sign the %v certificate successfully.\n", nothing.ClientSubjectName)
}
