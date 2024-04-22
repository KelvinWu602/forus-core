package p2p

type ProtocolMessageType uint

const (
	QueryPathRequest ProtocolMessageType = iota
	QueryPathResponse

	VerifyCoverRequest
	VerifyCoverResponse

	ConnectPathRequest
	ConnectPathResponse

	CreateProxyRequest
	CreateProxyResponse

	DeleteCoverRequest
)

// ProtocolMessage is the messages being sent to other nodes during the tree formation process.
type ProtocolMessage struct {
	Type    ProtocolMessageType
	Content []byte
}

type QueryPathReq struct {
	CoverPublicKey []byte
}

type QueryPathResp struct {
	ParentPublicKey []byte
	Paths           []Path
}

type Path struct {
	EncryptedTreeUUID []byte
	NextHopIP         string
	NextNextHopIP     string
	ProxyPublicKey    []byte
}

type VerifyCoverReq struct {
	NextHopIP string
}

type VerifyCoverResp struct {
	IsVerified bool
}

type ConnectPathReq struct {
	EncryptedTreeUUID []byte
	CoverKeyExchange  DHKeyExchange
}

type ConnectPathResp struct {
	Accepted          bool
	ParentKeyExchange DHKeyExchange
}

type CreateProxyReq struct {
	CoverPublicKey   []byte
	CoverKeyExchange DHKeyExchange
}

type CreateProxyResp struct {
	Accepted          bool
	ProxyKeyExchange  DHKeyExchange
	ProxyPublicKey    []byte
	EncryptedTreeUUID []byte
}
