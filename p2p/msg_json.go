package p2p

import "github.com/google/uuid"

// Data Schemas for all HTTP endpoints

type HTTPPostMessageReq struct {
	Content []byte    `json:"content"`
	PathID  uuid.UUID `json:"path_id,omitempty"`
}

type HTTPPostMessagesReq struct {
	Keys [][]byte `json:"keys"`
}

type HTTPPostMessageResp struct {
	PublishJobId uuid.UUID `json:"publish_job_id,omitempty"`
}

type HTTPPostPathReq struct {
	IP     string    `json:"ip,omitempty"`
	PathID uuid.UUID `json:"path_id,omitempty"`
}

type HTTPSchemaKeyMessage struct {
	Key     []byte `json:"key"`
	Content []byte `json:"content"`
}

type HTTPSchemaMessage struct {
	Content []byte `json:"content"`
}

type HTTPSchemaCoverNode struct {
	CoverIP            string    `json:"cover_ip"`
	SymmetricKeyInByte []byte    `json:"symmetric_key"`
	ConnectedPathId    uuid.UUID `json:"connected_path_id"`
}

type HTTPSchemaPublishJob struct {
	Key     []byte    `json:"message_key"`
	Status  string    `json:"status"`
	ViaPath uuid.UUID `json:"via_path"`
}

type HTTPSchemaPathAnalytics struct {
	SuccessCount int `json:"success_count"`
	FailureCount int `json:"failure_count"`
}

type HTTPSchemaPath struct {
	Id                 uuid.UUID               `json:"id"`
	Next               string                  `json:"next_hop_ip"`
	Next2              string                  `json:"next_next_hop_ip"`
	ProxyPublicKey     []byte                  `json:"proxy_public_key"`
	SymmetricKeyInByte []byte                  `json:"symmetric_key"`
	Analytics          HTTPSchemaPathAnalytics `json:"analytics"`
}

type HTTPSchemaKeyPair struct {
	Pub []byte `json:"public_key"`
	Pri []byte `json:"private_key"`
}

type HTTPSchemaPublishJobID struct {
	ID uuid.UUID `json:"publish_job_id"`
}
