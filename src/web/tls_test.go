// Tests that serve speaks HTTPS when given a certificate and marks the
// session cookie Secure.

package web

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestServeTLS(t *testing.T) {
	base := newTestServer(t, true)
	opts := base.opts
	opts.TLSCert, opts.TLSKey = writeCert(t, t.TempDir())
	// Built the way serve builds it: TLS options must make cookies Secure.
	s := New(base.db, base.sched, opts)
	s.opts.Listen = "127.0.0.1:0"
	l, err := s.Listen()
	if err != nil {
		t.Fatal(err)
	}
	s.opts.Listen = l.Addr().String()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- s.Serve(ctx, l) }()
	defer func() {
		cancel()
		if err := <-done; err != nil {
			t.Errorf("serve: %v", err)
		}
	}()

	client := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}}
	var resp *http.Response
	for i := 0; i < 50; i++ {
		req, _ := http.NewRequest("POST", "https://"+s.opts.Listen+"/api/login", strings.NewReader(`{"password":"`+password+`"}`))
		req.Header.Set(apiHeader, "1")
		resp, err = client.Do(req)
		if err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent || resp.TLS == nil || len(resp.Cookies()) != 1 || !resp.Cookies()[0].Secure {
		t.Fatalf("status %d tls %v cookies %+v", resp.StatusCode, resp.TLS != nil, resp.Cookies())
	}
	plain, err := http.Get("http://" + s.opts.Listen + "/api/session")
	if err == nil && plain.StatusCode == http.StatusOK {
		t.Fatal("plain HTTP served next to TLS")
	}
}

// writeCert writes a self-signed certificate for 127.0.0.1 and its key.
func writeCert(t *testing.T, dir string) (string, string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "mitosu test"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		IPAddresses:  []net.IP{net.IPv4(127, 0, 0, 1)},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	cert := filepath.Join(dir, "cert.pem")
	keyFile := filepath.Join(dir, "key.pem")
	if err := os.WriteFile(cert, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyFile, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}), 0o600); err != nil {
		t.Fatal(err)
	}
	return cert, keyFile
}

func TestListenFailsEarly(t *testing.T) {
	base := newTestServer(t, false)
	opts := base.opts
	opts.TLSCert, opts.TLSKey = "/nonexistent/cert.pem", "/nonexistent/key.pem"
	if _, err := New(base.db, base.sched, opts).Listen(); err == nil {
		t.Fatal("a missing certificate was accepted")
	}
	taken, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer taken.Close()
	opts = base.opts
	opts.Listen = taken.Addr().String()
	if _, err := New(base.db, base.sched, opts).Listen(); err == nil {
		t.Fatal("a taken port was accepted")
	}
}
