package agent

import (
	"crypto/rsa"
	"crypto/rand"
	"crypto/x509"
	"golang.org/x/crypto/ssh"
	"encoding/pem"
	"bytes"
	"bufio"
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
