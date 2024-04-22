package helpers

import (
	"bytes"
	"crypto/tls"
	"fmt"
	"net"
	"time"

	corev1 "k8s.io/api/core/v1"
)

// tlsEchoResponds takes a TLS address URL and a Pod name and checks if a go-echo
// instance is running on that Pod at that address using hostname for SNI. It sends
// a message and checks if returned one matches. It returns an error with
// an explanation if it is not (typical network related errors like io.EOF or
// syscall.ECONNRESET are returned directly).
//
// This is copy-paste from the test package in the kong/kubernetes-ingress-controller repo
// https://github.com/Kong/kubernetes-ingress-controller/tree/main/test/integration/tlsroute_test.go#L709
func TLSEchoResponds(
	url string, podName string, certHostname string, tlsSecret *corev1.Secret, passthrough bool,
) error {
	tlsConfig, err := createTLSClientConfig(tlsSecret, certHostname)
	if err != nil {
		return err
	}
	tlsConfig.InsecureSkipVerify = true
	dialer := net.Dialer{Timeout: time.Second * 10}
	conn, err := tls.DialWithDialer(&dialer,
		"tcp",
		url,
		tlsConfig,
	)
	if err != nil {
		return err
	}
	defer conn.Close()

	cert := conn.ConnectionState().PeerCertificates[0]
	if cert.Subject.CommonName != certHostname {
		return fmt.Errorf("expected certificate with cn=%s, got cn=%s", certHostname, cert.Subject.CommonName)
	}

	header := []byte(fmt.Sprintf("Running on Pod %s.", podName))
	// if we are testing with passthrough, the go-echo service should return a message
	// noting that it is listening in TLS mode.
	if passthrough {
		header = append(header, []byte("\nThrough TLS connection.")...)
	}
	message := []byte("testing tlsroute")

	wrote, err := conn.Write(message)
	if err != nil {
		return err
	}

	if wrote != len(message) {
		return fmt.Errorf("wrote message of size %d, expected %d", wrote, len(message))
	}

	if err := conn.SetDeadline(time.Now().Add(time.Second * 5)); err != nil {
		return err
	}

	headerResponse := make([]byte, len(header)+1)
	read, err := conn.Read(headerResponse)
	if err != nil {
		return err
	}

	if read != len(header)+1 { // add 1 for newline
		return fmt.Errorf("read %d bytes but expected %d", read, len(header)+1)
	}

	if !bytes.Contains(headerResponse, header) {
		return fmt.Errorf(`expected header response "%s", received: "%s"`, string(header), string(headerResponse))
	}

	messageResponse := make([]byte, wrote+1)
	read, err = conn.Read(messageResponse)
	if err != nil {
		return err
	}

	if read != len(message) {
		return fmt.Errorf("read %d bytes but expected %d", read, len(message))
	}

	if !bytes.Contains(messageResponse, message) {
		return fmt.Errorf(`expected message response "%s", received: "%s"`, string(message), string(messageResponse))
	}

	return nil
}
