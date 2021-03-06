// Copyright 2014 ISRG.  All rights reserved
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package va

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	jose "github.com/letsencrypt/boulder/Godeps/_workspace/src/github.com/square/go-jose"
	"github.com/letsencrypt/boulder/core"
	"github.com/letsencrypt/boulder/test"
)

func bigIntFromB64(b64 string) *big.Int {
	bytes, _ := base64.URLEncoding.DecodeString(b64)
	x := big.NewInt(0)
	x.SetBytes(bytes)
	return x
}

func intFromB64(b64 string) int {
	return int(bigIntFromB64(b64).Int64())
}

var n *big.Int = bigIntFromB64("n4EPtAOCc9AlkeQHPzHStgAbgs7bTZLwUBZdR8_KuKPEHLd4rHVTeT-O-XV2jRojdNhxJWTDvNd7nqQ0VEiZQHz_AJmSCpMaJMRBSFKrKb2wqVwGU_NsYOYL-QtiWN2lbzcEe6XC0dApr5ydQLrHqkHHig3RBordaZ6Aj-oBHqFEHYpPe7Tpe-OfVfHd1E6cS6M1FZcD1NNLYD5lFHpPI9bTwJlsde3uhGqC0ZCuEHg8lhzwOHrtIQbS0FVbb9k3-tVTU4fg_3L_vniUFAKwuCLqKnS2BYwdq_mzSnbLY7h_qixoR7jig3__kRhuaxwUkRz5iaiQkqgc5gHdrNP5zw==")
var e int = intFromB64("AQAB")
var d *big.Int = bigIntFromB64("bWUC9B-EFRIo8kpGfh0ZuyGPvMNKvYWNtB_ikiH9k20eT-O1q_I78eiZkpXxXQ0UTEs2LsNRS-8uJbvQ-A1irkwMSMkK1J3XTGgdrhCku9gRldY7sNA_AKZGh-Q661_42rINLRCe8W-nZ34ui_qOfkLnK9QWDDqpaIsA-bMwWWSDFu2MUBYwkHTMEzLYGqOe04noqeq1hExBTHBOBdkMXiuFhUq1BU6l-DqEiWxqg82sXt2h-LMnT3046AOYJoRioz75tSUQfGCshWTBnP5uDjd18kKhyv07lhfSJdrPdM5Plyl21hsFf4L_mHCuoFau7gdsPfHPxxjVOcOpBrQzwQ==")
var p *big.Int = bigIntFromB64("uKE2dh-cTf6ERF4k4e_jy78GfPYUIaUyoSSJuBzp3Cubk3OCqs6grT8bR_cu0Dm1MZwWmtdqDyI95HrUeq3MP15vMMON8lHTeZu2lmKvwqW7anV5UzhM1iZ7z4yMkuUwFWoBvyY898EXvRD-hdqRxHlSqAZ192zB3pVFJ0s7pFc=")
var q *big.Int = bigIntFromB64("uKE2dh-cTf6ERF4k4e_jy78GfPYUIaUyoSSJuBzp3Cubk3OCqs6grT8bR_cu0Dm1MZwWmtdqDyI95HrUeq3MP15vMMON8lHTeZu2lmKvwqW7anV5UzhM1iZ7z4yMkuUwFWoBvyY898EXvRD-hdqRxHlSqAZ192zB3pVFJ0s7pFc=")

var TheKey rsa.PrivateKey = rsa.PrivateKey{
	PublicKey: rsa.PublicKey{N: n, E: e},
	D:         d,
	Primes:    []*big.Int{p, q},
}

var ident core.AcmeIdentifier = core.AcmeIdentifier{Type: core.IdentifierType("dns"), Value: "localhost"}

const expectedToken = "THETOKEN"
const pathWrongToken = "wrongtoken"
const path404 = "404"

func simpleSrv(t *testing.T, token string, stopChan, waitChan chan bool) {
	// Reset any existing handlers
	http.DefaultServeMux = http.NewServeMux()

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, path404) {
			t.Logf("SIMPLESRV: Got a 404 req\n")
			http.NotFound(w, r)
		} else if strings.HasSuffix(r.URL.Path, pathWrongToken) {
			t.Logf("SIMPLESRV: Got a wrongtoken req\n")
			fmt.Fprintf(w, "wrongtoken")
		} else {
			t.Logf("SIMPLESRV: Got a valid req\n")
			fmt.Fprintf(w, "%s", token)
		}
	})

	httpsServer := &http.Server{Addr: "localhost:5001"}
	conn, err := net.Listen("tcp", httpsServer.Addr)
	if err != nil {
		waitChan <- true
		t.Fatalf("Couldn't listen on %s: %s", httpsServer.Addr, err)
	}

	go func() {
		<-stopChan
		conn.Close()
	}()

	waitChan <- true
	t.Fatalf("%s", httpsServer.Serve(conn))
}

func dvsniSrv(t *testing.T, R, S []byte, stopChan, waitChan chan bool) {
	RS := append(R, S...)
	z := sha256.Sum256(RS)
	zName := fmt.Sprintf("%064x.acme.invalid", z)
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1337),
		Subject: pkix.Name{
			Organization: []string{"tests"},
		},
		NotBefore: time.Now(),
		NotAfter:  time.Now().AddDate(0, 0, 1),

		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,

		DNSNames: []string{zName},
	}

	certBytes, _ := x509.CreateCertificate(rand.Reader, template, template, &TheKey.PublicKey, &TheKey)
	cert := &tls.Certificate{
		Certificate: [][]byte{certBytes},
		PrivateKey:  &TheKey,
	}

	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{*cert},
		ClientAuth:   tls.NoClientCert,
		GetCertificate: func(clientHello *tls.ClientHelloInfo) (*tls.Certificate, error) {
			fmt.Println(clientHello)
			return cert, nil
		},
		NextProtos: []string{"http/1.1"},
	}

	httpsServer := &http.Server{Addr: "localhost:5001"}
	conn, err := net.Listen("tcp", httpsServer.Addr)
	if err != nil {
		waitChan <- true
		t.Fatalf("Couldn't listen on %s: %s", httpsServer.Addr, err)
	}
	tlsListener := tls.NewListener(conn, tlsConfig)

	go func() {
		<-stopChan
		conn.Close()
	}()

	waitChan <- true
	t.Fatalf("%s", httpsServer.Serve(tlsListener))
}

func TestSimpleHttps(t *testing.T) {
	va := NewValidationAuthorityImpl(true)

	chall := core.Challenge{Path: "test", Token: expectedToken}

	invalidChall, err := va.validateSimpleHTTPS(ident, chall)
	test.AssertEquals(t, invalidChall.Status, core.StatusInvalid)
	test.AssertError(t, err, "Server's not up yet; expected refusal. Where did we connect?")

	stopChan := make(chan bool, 1)
	waitChan := make(chan bool, 1)
	go simpleSrv(t, expectedToken, stopChan, waitChan)
	defer func() { stopChan <- true }()
	<-waitChan

	finChall, err := va.validateSimpleHTTPS(ident, chall)
	test.AssertEquals(t, finChall.Status, core.StatusValid)
	test.AssertNotError(t, err, chall.Path)

	chall.Path = path404
	invalidChall, err = va.validateSimpleHTTPS(ident, chall)
	test.AssertEquals(t, invalidChall.Status, core.StatusInvalid)
	test.AssertError(t, err, "Should have found a 404 for the challenge.")

	chall.Path = pathWrongToken
	invalidChall, err = va.validateSimpleHTTPS(ident, chall)
	test.AssertEquals(t, invalidChall.Status, core.StatusInvalid)
	test.AssertError(t, err, "The path should have given us the wrong token.")

	chall.Path = ""
	invalidChall, err = va.validateSimpleHTTPS(ident, chall)
	test.AssertEquals(t, invalidChall.Status, core.StatusInvalid)
	test.AssertError(t, err, "Empty paths shouldn't work either.")

	chall.Path = "validish"
	invalidChall, err = va.validateSimpleHTTPS(core.AcmeIdentifier{Type: core.IdentifierType("ip"), Value: "127.0.0.1"}, chall)
	test.AssertEquals(t, invalidChall.Status, core.StatusInvalid)
	test.AssertError(t, err, "IdentifierType IP shouldn't have worked.")
}

func TestDvsni(t *testing.T) {
	va := NewValidationAuthorityImpl(true)

	a := []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 0}
	ba := core.B64enc(a)
	chall := core.Challenge{R: ba, S: ba}

	invalidChall, err := va.validateDvsni(ident, chall)
	test.AssertEquals(t, invalidChall.Status, core.StatusInvalid)
	test.AssertError(t, err, "Server's not up yet; expected refusal. Where did we connect?")

	waitChan := make(chan bool, 1)
	stopChan := make(chan bool, 1)
	go dvsniSrv(t, a, a, stopChan, waitChan)
	defer func() { stopChan <- true }()
	<-waitChan

	finChall, err := va.validateDvsni(ident, chall)
	test.AssertEquals(t, finChall.Status, core.StatusValid)
	test.AssertNotError(t, err, "")

	chall.R = ba[5:]
	invalidChall, err = va.validateDvsni(ident, chall)
	test.AssertEquals(t, invalidChall.Status, core.StatusInvalid)
	test.AssertError(t, err, "R Should be illegal Base64")

	invalidChall, err = va.validateSimpleHTTPS(core.AcmeIdentifier{Type: core.IdentifierType("ip"), Value: "127.0.0.1"}, chall)
	test.AssertEquals(t, invalidChall.Status, core.StatusInvalid)
	test.AssertError(t, err, "Forgot path; that should be an error.")

	chall.R = ba
	chall.S = "!@#"
	invalidChall, err = va.validateDvsni(ident, chall)
	test.AssertEquals(t, invalidChall.Status, core.StatusInvalid)
	test.AssertError(t, err, "S Should be illegal Base64")
}

func TestValidateHTTPS(t *testing.T) {
	va := NewValidationAuthorityImpl(true)
	mockRA := &MockRegistrationAuthority{}
	va.RA = mockRA

	challHTTPS := core.SimpleHTTPSChallenge()
	challHTTPS.Path = "test"

	stopChanHTTPS := make(chan bool, 1)
	waitChanHTTPS := make(chan bool, 1)
	go simpleSrv(t, challHTTPS.Token, stopChanHTTPS, waitChanHTTPS)

	// Let them start
	<-waitChanHTTPS

	// shutdown cleanly
	defer func() {
		stopChanHTTPS <- true
	}()

	var authz = core.Authorization{
		ID:             core.NewToken(),
		RegistrationID: 1,
		Identifier:     ident,
		Challenges:     []core.Challenge{challHTTPS},
	}
	va.validate(authz)

	test.AssertEquals(t, core.StatusValid, mockRA.lastAuthz.Challenges[0].Status)
}

func TestValidateDvsni(t *testing.T) {
	va := NewValidationAuthorityImpl(true)
	mockRA := &MockRegistrationAuthority{}
	va.RA = mockRA

	challDvsni := core.DvsniChallenge()
	challDvsni.S = challDvsni.R

	waitChanDvsni := make(chan bool, 1)
	stopChanDvsni := make(chan bool, 1)
	ar, _ := core.B64dec(challDvsni.R)
	as, _ := core.B64dec(challDvsni.S)
	go dvsniSrv(t, ar, as, stopChanDvsni, waitChanDvsni)

	// Let them start
	<-waitChanDvsni

	// shutdown cleanly
	defer func() {
		stopChanDvsni <- true
	}()

	var authz = core.Authorization{
		ID:             core.NewToken(),
		RegistrationID: 1,
		Identifier:     ident,
		Challenges:     []core.Challenge{challDvsni},
	}
	va.validate(authz)

	test.AssertEquals(t, core.StatusValid, mockRA.lastAuthz.Challenges[0].Status)
}

func TestValidateDvsniNotSane(t *testing.T) {
	va := NewValidationAuthorityImpl(true)
	mockRA := &MockRegistrationAuthority{}
	va.RA = mockRA

	challDvsni := core.DvsniChallenge()
	challDvsni.R = "boulder" // Not a sane thing to do.

	waitChanDvsni := make(chan bool, 1)
	stopChanDvsni := make(chan bool, 1)
	ar, _ := core.B64dec(challDvsni.R)
	as, _ := core.B64dec(challDvsni.S)
	go dvsniSrv(t, ar, as, stopChanDvsni, waitChanDvsni)

	// Let them start
	<-waitChanDvsni

	// shutdown cleanly
	defer func() {
		stopChanDvsni <- true
	}()

	var authz = core.Authorization{
		ID:             core.NewToken(),
		RegistrationID: 1,
		Identifier:     ident,
		Challenges:     []core.Challenge{challDvsni},
	}
	va.validate(authz)

	test.AssertEquals(t, core.StatusInvalid, mockRA.lastAuthz.Challenges[0].Status)
}

func TestUpdateValidations(t *testing.T) {
	va := NewValidationAuthorityImpl(true)
	mockRA := &MockRegistrationAuthority{}
	va.RA = mockRA

	var authz = core.Authorization{
		ID:             core.NewToken(),
		RegistrationID: 1,
		Identifier:     ident,
		Challenges:     []core.Challenge{},
	}

	va.UpdateValidations(authz)

	// Nothing to assert.
}

type MockRegistrationAuthority struct {
	lastAuthz *core.Authorization
}

func (ra *MockRegistrationAuthority) NewRegistration(reg core.Registration, jwk jose.JsonWebKey) (core.Registration, error) {
	return reg, nil
}

func (ra *MockRegistrationAuthority) NewAuthorization(authz core.Authorization, regID int64) (core.Authorization, error) {
	return authz, nil
}

func (ra *MockRegistrationAuthority) NewCertificate(req core.CertificateRequest, regID int64) (core.Certificate, error) {
	return core.Certificate{}, nil
}

func (ra *MockRegistrationAuthority) UpdateRegistration(reg core.Registration, updated core.Registration) (core.Registration, error) {
	return reg, nil
}

func (ra *MockRegistrationAuthority) UpdateAuthorization(authz core.Authorization, foo int, challenge core.Challenge) (core.Authorization, error) {
	return authz, nil
}

func (ra *MockRegistrationAuthority) RevokeCertificate(cert x509.Certificate) error {
	return nil
}

func (ra *MockRegistrationAuthority) OnValidationUpdate(authz core.Authorization) error {
	ra.lastAuthz = &authz
	return nil
}
