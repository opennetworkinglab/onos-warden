package agent

import (
	"crypto/rand"
	"golang.org/x/crypto/ssh"
	"testing"
)

func TestGen(t *testing.T) {
	private, public, err := GenerateKeyPair()
	if err != nil {
		t.Error(err)
	}

	privateKey, err := ssh.ParsePrivateKey([]byte(private))
	if err != nil {
		t.Error(err)
	}

	publicKey, _, _, _, err := ssh.ParseAuthorizedKey([]byte(public))
	if err != nil {
		t.Error(err)
	}

	m := []byte("test message")
	sig, err := privateKey.Sign(rand.Reader, m)
	if err != nil {
		t.Error(err)
	}

	err = publicKey.Verify(m, sig)
	if err != nil {
		t.Error(err)
	}
}
