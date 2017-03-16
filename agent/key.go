package agent

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"golang.org/x/crypto/ssh"
)

const KeyLength = 2048 //bits

func GenerateKeyPair() (privateKey, publicKey string, err error) {
	// Generate key pair
	pk, err := rsa.GenerateKey(rand.Reader, KeyLength)
	if err != nil {
		return
	}

	// Format private key
	var b bytes.Buffer
	w := bufio.NewWriter(&b)
	err = pem.Encode(w, &pem.Block{Type: "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(pk)})
	if err != nil {
		return
	}
	w.Flush()
	privateKey = string(b.Bytes())

	// Format public key
	pub, err := ssh.NewPublicKey(pk.Public())
	if err != nil {
		return
	}
	publicKey = string(ssh.MarshalAuthorizedKey(pub))
	return
}
