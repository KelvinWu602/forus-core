package p2p

import (
	"encoding/base64"
	"testing"

	"github.com/KelvinWu602/immutable-storage/blueprint"
)

// type message struct {
// 	// uuid is the first 16 bytes of the message key, which can be random values.
// 	uuid [16]byte
// 	// checksum is a SHA256 hash of concat(uuid,payload).
// 	checksum [32]byte
// 	// payload is the actual message content stored by upper application.
// 	payload []byte
// }

func TestValidateKey(t *testing.T) {
	key, _ := base64.URLEncoding.DecodeString("n12BW68CQf443flTP8YIAE6r8Ah1oGmevAQW46_T5PuHjvzgVgn08cboOOjHTz1s")
	message, _ := base64.StdEncoding.DecodeString("n12BW68CQf443flTP8YIAE6r8Ah1oGmevAQW46/T5PuHjvzgVgn08cboOOjHTz1sewoJCSJoZWxsbyI6ICIxIiwKCQkieW9vb28iOiAyCgl9")
	if !blueprint.ValidateKey(blueprint.Key(key[:]), message) {
		t.Fatal("should ok")
	}
}

func useTestMoveUpNodeConfigs() {
	// avoid fulfillPublishCondition
}

func TestMoveUpWhenNextHopNotProxySuccess(t *testing.T) {
	// expected to connect to next next hop

}

func TestMoveUpWhenNextHopNotProxyVerifyFail(t *testing.T) {
	// expected to self become proxy
}

func TestMoveUpWhenNextHopNotProxyVerifyOkConnectFail(t *testing.T) {
	// expected to self become proxy
}
