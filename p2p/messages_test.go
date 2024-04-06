package p2p

import (
	"bytes"
	"encoding/gob"
	"math/big"
	"testing"

	"golang.org/x/exp/slices"
)

func checkCodec[T any](encode T, empty *T) error {
	buf := bytes.NewBuffer([]byte{})
	if err := gob.NewEncoder(buf).Encode(encode); err != nil {
		return err
	}
	if err := gob.NewDecoder(buf).Decode(&empty); err != nil {
		return err
	}
	return nil
}

func TestQueryPathReq(t *testing.T) {
	dummy := QueryPathReq{
		PublicKey: []byte{1, 2, 3},
	}
	empty := QueryPathReq{}
	err := checkCodec(dummy, &empty)
	if err != nil {
		t.Fatalf("QueryPathReq codec error: %v\n", err)
	}
	if !slices.Equal(dummy.PublicKey, empty.PublicKey) {
		t.Fatalf("QueryPathReq codec unmatch:\n%v\n%v", dummy, empty)
	}
	t.Logf("Should see same values: %v %v", dummy, empty)
}

func TestDHKeyExchange(t *testing.T) {
	dummy := DHKeyExchange{
		G:       big.NewInt(2345675),
		P:       big.NewInt(4565678789),
		HalfKey: big.NewInt(765432356787654),
	}
	empty := DHKeyExchange{}
	err := checkCodec(dummy, &empty)
	if err != nil {
		t.Fatalf("DHKeyExchange codec error: %v\n", err)
	}
	if dummy.G.Cmp(empty.G) != 0 || dummy.P.Cmp(empty.P) != 0 || dummy.HalfKey.Cmp(empty.HalfKey) != 0 {
		t.Fatalf("DHKeyExchange codec value unmatch:\n%v\n%v", dummy, empty)
	}
	t.Logf("Should see same values: %v %v", dummy, empty)

	// try edit empty's value, should no longer equal to dummy's value
	empty.G.Set(big.NewInt(3))

	if dummy.G.Cmp(empty.G) == 0 {
		t.Fatal("DHKeyExchange codec refer to same underlying big.Int")
	}

	t.Logf("Should see different values: %v %v", dummy, empty)
}

func TestConnectPathReq(t *testing.T) {
	dummy := ConnectPathReq{
		EncryptedTreeUUID: []byte{1, 2, 3},
		KeyExchange: DHKeyExchange{
			G:       big.NewInt(2345675),
			P:       big.NewInt(4565678789),
			HalfKey: big.NewInt(765432356787654),
		},
	}
	empty := ConnectPathReq{}
	err := checkCodec(dummy, &empty)
	if err != nil {
		t.Fatalf("ConnectPathReq codec error: %v\n", err)
	}
	if !slices.Equal(dummy.EncryptedTreeUUID, empty.EncryptedTreeUUID) {
		t.Fatalf("ConnectPathReq codec unmatch:\n%v\n%v", dummy, empty)
	}
	dKE := dummy.KeyExchange
	eKE := empty.KeyExchange
	if dKE.G.Cmp(eKE.G) != 0 || dKE.P.Cmp(eKE.P) != 0 || dKE.HalfKey.Cmp(eKE.HalfKey) != 0 {
		t.Fatalf("ConnectPathReq codec unmatch:\n%v\n%v", dummy, empty)
	}
	t.Logf("Should see same values: %v %v", dummy, empty)
}

func TestCreateProxyReq(t *testing.T) {
	dummy := CreateProxyReq{
		PublicKey: []byte{1, 2, 3},
		KeyExchange: DHKeyExchange{
			G:       big.NewInt(2345675),
			P:       big.NewInt(4565678789),
			HalfKey: big.NewInt(765432356787654),
		},
	}
	empty := CreateProxyReq{}
	err := checkCodec(dummy, &empty)
	if err != nil {
		t.Fatalf("CreateProxyReq codec error: %v\n", err)
	}
	if !slices.Equal(dummy.PublicKey, empty.PublicKey) {
		t.Fatalf("CreateProxyReq codec unmatch:\n%v\n%v", dummy, empty)
	}
	dKE := dummy.KeyExchange
	eKE := empty.KeyExchange
	if dKE.G.Cmp(eKE.G) != 0 || dKE.P.Cmp(eKE.P) != 0 || dKE.HalfKey.Cmp(eKE.HalfKey) != 0 {
		t.Fatalf("CreateProxyReq codec unmatch:\n%v\n%v", dummy, empty)
	}
	t.Logf("Should see same values: %v %v", dummy, empty)
}

func TestPath(t *testing.T) {
	dummy := Path{
		EncryptedTreeUUID: []byte{1, 2, 3, 4},
		NextHop:           "Dick",
		NextNextHop:       "Shit",
		ProxyPublicKey:    []byte{5, 6, 7, 8},
	}
	empty := Path{}
	err := checkCodec(dummy, &empty)
	if err != nil {
		t.Fatalf("Path codec error: %v\n", err)
	}
	if !slices.Equal(dummy.EncryptedTreeUUID, empty.EncryptedTreeUUID) {
		t.Fatalf("Path codec unmatch:\n%v\n%v", dummy, empty)
	}
	if !slices.Equal(dummy.ProxyPublicKey, empty.ProxyPublicKey) {
		t.Fatalf("Path codec unmatch:\n%v\n%v", dummy, empty)
	}
	if dummy.NextHop != empty.NextHop || dummy.NextNextHop != empty.NextNextHop {
		t.Fatalf("Path codec unmatch:\n%v\n%v", dummy, empty)
	}
	t.Logf("Should see same values: %v %v", dummy, empty)
}

func TestQueryPathResp(t *testing.T) {
	dummy := QueryPathResp{
		NodePublicKey: []byte{1, 2, 3, 4},
		Paths: []Path{
			{
				EncryptedTreeUUID: []byte{1, 2, 3, 4},
				NextHop:           "Dick",
				NextNextHop:       "Shit",
				ProxyPublicKey:    []byte{5, 6, 7, 8},
			},
		},
	}
	empty := QueryPathResp{}
	err := checkCodec(dummy, &empty)
	if err != nil {
		t.Fatalf("QueryPathResp codec error: %v\n", err)
	}
	if !slices.Equal(dummy.NodePublicKey, empty.NodePublicKey) {
		t.Fatalf("QueryPathResp codec unmatch:\n%v\n%v", dummy, empty)
	}
	dP := dummy.Paths[0]
	eP := empty.Paths[0]

	if !slices.Equal(dP.EncryptedTreeUUID, eP.EncryptedTreeUUID) {
		t.Fatalf("QueryPathResp codec unmatch:\n%v\n%v", dummy, empty)
	}
	if !slices.Equal(dP.ProxyPublicKey, eP.ProxyPublicKey) {
		t.Fatalf("QueryPathResp codec unmatch:\n%v\n%v", dummy, empty)
	}
	if dP.NextHop != eP.NextHop || dP.NextNextHop != eP.NextNextHop {
		t.Fatalf("QueryPathResp codec unmatch:\n%v\n%v", dummy, empty)
	}
	t.Logf("Should see same values: %v %v", dummy, empty)
}

func TestVerifyCoverReq(t *testing.T) {
	dummy := VerifyCoverReq{
		NextHop: "123",
	}
	empty := VerifyCoverReq{}
	err := checkCodec(dummy, &empty)
	if err != nil {
		t.Fatalf("VerifyCoverReq codec error: %v\n", err)
	}
	if dummy.NextHop != empty.NextHop {
		t.Fatalf("VerifyCoverReq codec unmatch:\n%v\n%v", dummy, empty)
	}
	t.Logf("Should see same values: %v %v", dummy, empty)
}

func TestVerifyCoverResp(t *testing.T) {
	dummy := VerifyCoverResp{
		IsVerified: true,
	}
	empty := VerifyCoverResp{}
	err := checkCodec(dummy, &empty)
	if err != nil {
		t.Fatalf("VerifyCoverResp codec error: %v\n", err)
	}
	if dummy.IsVerified != empty.IsVerified {
		t.Fatalf("VerifyCoverResp codec unmatch:\n%v\n%v", dummy, empty)
	}
	t.Logf("Should see same values: %v %v", dummy, empty)
}

func TestConnectPathResp(t *testing.T) {
	dummy := ConnectPathResp{
		Status: true,
		KeyExchange: DHKeyExchange{
			G:       big.NewInt(2345675),
			P:       big.NewInt(4565678789),
			HalfKey: big.NewInt(765432356787654),
		},
	}
	empty := ConnectPathResp{}
	err := checkCodec(dummy, &empty)
	if err != nil {
		t.Fatalf("ConnectPathResp codec error: %v\n", err)
	}
	if dummy.Status != empty.Status {
		t.Fatalf("ConnectPathResp codec unmatch:\n%v\n%v", dummy, empty)
	}
	dKE := dummy.KeyExchange
	eKE := empty.KeyExchange
	if dKE.G.Cmp(eKE.G) != 0 || dKE.P.Cmp(eKE.P) != 0 || dKE.HalfKey.Cmp(eKE.HalfKey) != 0 {
		t.Fatalf("ConnectPathResp codec unmatch:\n%v\n%v", dummy, empty)
	}
	t.Logf("Should see same values: %v %v", dummy, empty)
}

func TestCreateProxyResp(t *testing.T) {
	dummy := CreateProxyResp{
		Status: true,
		KeyExchange: DHKeyExchange{
			G:       big.NewInt(2345675),
			P:       big.NewInt(4565678789),
			HalfKey: big.NewInt(765432356787654),
		},
		Public:            []byte{1, 2, 3},
		EncryptedTreeUUID: []byte{1, 2, 3},
	}
	empty := CreateProxyResp{}
	err := checkCodec(dummy, &empty)
	if err != nil {
		t.Fatalf("CreateProxyResp codec error: %v\n", err)
	}
	if dummy.Status != empty.Status {
		t.Fatalf("CreateProxyResp codec unmatch:\n%v\n%v", dummy, empty)
	}
	if !slices.Equal(dummy.Public, empty.Public) || !slices.Equal(dummy.EncryptedTreeUUID, empty.EncryptedTreeUUID) {
		t.Fatalf("CreateProxyResp codec unmatch:\n%v\n%v", dummy, empty)
	}
	dKE := dummy.KeyExchange
	eKE := empty.KeyExchange
	if dKE.G.Cmp(eKE.G) != 0 || dKE.P.Cmp(eKE.P) != 0 || dKE.HalfKey.Cmp(eKE.HalfKey) != 0 {
		t.Fatalf("CreateProxyResp codec unmatch:\n%v\n%v", dummy, empty)
	}
	t.Logf("Should see same values: %v %v", dummy, empty)
}

func TestProtocolMessage(t *testing.T) {
	dummy := CreateProxyResp{
		Status: true,
		KeyExchange: DHKeyExchange{
			G:       big.NewInt(2345675),
			P:       big.NewInt(4565678789),
			HalfKey: big.NewInt(765432356787654),
		},
		Public:            []byte{1, 2, 3},
		EncryptedTreeUUID: []byte{1, 2, 3},
	}

	buffer := bytes.NewBuffer([]byte{})
	if gob.NewEncoder(buffer).Encode(dummy) != nil {
		t.Fatal("Failed to encode dummy into bytes")
	}

	dummyP := ProtocolMessage{
		Type:    ConnectPathRequest,
		Content: buffer.Bytes(),
	}

	emptyP := ProtocolMessage{}
	empty := CreateProxyResp{}
	err := checkCodec(dummyP, &emptyP)
	if err != nil {
		t.Fatalf("ProtocolMessage codec error: %v\n", err)
	}
	if dummyP.Type != emptyP.Type {
		t.Fatalf("ProtocolMessage codec unmatch:\n%v\n%v", dummyP, emptyP)
	}
	if !slices.Equal(dummyP.Content, emptyP.Content) {
		t.Fatalf("ProtocolMessage codec unmatch:\n%v\n%v", dummyP, emptyP)
	}

	if gob.NewDecoder(bytes.NewBuffer(dummyP.Content)).Decode(&empty) != nil {
		t.Fatalf("failed cast emptyP.Content back to CreateProxyResp object:\n%v\n%v\n", dummyP, emptyP)
	}
	if dummy.Status != empty.Status {
		t.Fatalf("CreateProxyResp codec unmatch:\n%v\n%v", dummy, empty)
	}
	if !slices.Equal(dummy.Public, empty.Public) || !slices.Equal(dummy.EncryptedTreeUUID, empty.EncryptedTreeUUID) {
		t.Fatalf("CreateProxyResp codec unmatch:\n%v\n%v", dummy, empty)
	}
	dKE := dummy.KeyExchange
	eKE := empty.KeyExchange
	if dKE.G.Cmp(eKE.G) != 0 || dKE.P.Cmp(eKE.P) != 0 || dKE.HalfKey.Cmp(eKE.HalfKey) != 0 {
		t.Fatalf("CreateProxyResp codec unmatch:\n%v\n%v", dummy, empty)
	}
	t.Logf("Should see same values: %v %v", dummy, empty)
}
