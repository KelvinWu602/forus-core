package p2p

import (
	"fmt"
	"net/http"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

// go test -timeout 120s -run ^TestBlacklistPathNextHopIsProxy$ github.com/KelvinWu602/forus-core/p2p -v -count=1
func TestBlacklistPathNextHopIsProxy(t *testing.T) {
	assert := assert.New(t)
	// Proxy (127.0.0.1)
	// Cover (127.0.0.2)

	// disable nodes auto connect paths
	os.Setenv("TARGET_NUMBER_OF_CONNECTED_PATHS", "0")

	// Defined in test/config-TestBlacklistPath-NextHopIsProxy-Cover.yaml
	// PUBLISH_JOB_FAILED_TIMEOUT: 8s
	// PUBLISH_JOB_CHECKING_INTERVAL: 3s
	// NUMBER_OF_COVER_NODES_FOR_PUBLISH: 0
	// TARGET_NUMBER_OF_CONNECTED_PATHS: 0
	// MOVE_UP_REQUIREMENT_FAILURE_THRESHOLD: 1
	// MAINTAIN_PATHS_HEALTH_CHECKING_INTERVAL: 2s

	var setup sync.WaitGroup
	setup.Add(3)
	// All component prefixed with 1 ==> Mock
	// All component prefixed with 2 ==> Normal
	members := map[string][]string{}
	members["127.0.0.1"] = []string{}
	members["127.0.0.2"] = []string{}
	NDlock := sync.RWMutex{}
	mockND1 := NewMockND("127.0.0.1", members, &NDlock)
	mockND2 := NewMockND("127.0.0.2", members, &NDlock)

	mockIS1 := NewMockIS()
	// mockIS2 does not share data with mockIS1, simulating when proxy drops the message
	mockIS2 := NewMockIS()

	var proxy, cover *Node

	go func() {
		initMockND(mockND1, "127.0.0.1:3200")
		initMockND(mockND2, "127.0.0.2:3200")
		initMockIS(mockIS1, "127.0.0.1:3100")
		initMockIS(mockIS2, "127.0.0.2:3100")
		setup.Done()
	}()

	go func() {
		proxy = StartNodeInternal("../unit_test_configs/config-TestBlacklistPath-NextHopIsProxy-Proxy.yaml")
		setup.Done()
	}()

	go func() {
		cover = StartNodeInternal("../unit_test_configs/config-TestBlacklistPath-NextHopIsProxy-Cover.yaml")
		setup.Done()
	}()
	setup.Wait()

	// http calls

	// post path on proxy
	assert.Equal(0, proxy.paths.getSize(), "proxy should have 0 paths at the beginning")
	postpathres, resBody := sendPostRequest("http://127.0.0.1:3000/path", `{}`)
	defer postpathres.Body.Close()
	if postpathres.StatusCode != http.StatusCreated {
		t.Fatal(postpathres.StatusCode, postpathres.Status, resBody, "post path on proxy wrong status code")
	}
	t.Log(resBody)
	assert.Equal(1, proxy.paths.getSize(), "proxy should have 1 paths after post /path")
	// need to get the pathID
	var newPathID uuid.UUID
	proxy.paths.iterate(func(u uuid.UUID, _ PathProfile) {
		newPathID = u
	}, true)

	// post path on cover
	assert.Equal(0, cover.paths.getSize(), "cover should have 0 paths at the beginning")
	body := fmt.Sprintf(`{"ip":"127.0.0.1","path_id":"%v"}`, newPathID.String())
	t.Log("post path on cover body", body)
	postpathres2, resBody := sendPostRequest("http://127.0.0.2:3000/path", body)
	defer postpathres2.Body.Close()
	if postpathres.StatusCode != http.StatusCreated {
		t.Fatal(postpathres.StatusCode, postpathres.Status, resBody, "post path on cover wrong status code")
	}
	t.Log(resBody)
	assert.Equal(1, cover.paths.getSize(), "cover should have 1 paths after post /path")
	assert.Equal(1, proxy.covers.getSize(), "proxy should have 1 cover after post /path")
	pathOnCover, _ := cover.paths.getValue(newPathID)
	coverProfile, found := proxy.covers.getValue("127.0.0.2")
	assert.Equal(true, found, "should find the cover profile")
	assert.Equal(coverProfile.treeUUID, pathOnCover.uuid, "should have the same tree id")
	t.Log("Proxy cover:", coverProfile)

	// post message on cover
	body = fmt.Sprintf(`{"content": "%v"}`, testmessage)
	postmsgres, resBody := sendPostRequest(fmt.Sprintf("http://127.0.0.2:3000/message/%v", testkey), body)
	defer postmsgres.Body.Close()
	if postmsgres.StatusCode != http.StatusCreated {
		t.Fatal(postmsgres.StatusCode, postmsgres.Status, resBody, "post msg on cover wrong status code")
	}
	t.Log(resBody)

	// Wait for at least PUBLISH_JOB_FAILED_TIMEOUT = 8s
	time.Sleep(10 * time.Second)

	// should not see this path on cover
	_, found = cover.paths.getValue(newPathID)
	assert.Equal(false, found, "should not see this path on cover")
}
