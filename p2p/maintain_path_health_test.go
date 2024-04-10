package p2p

import (
	"encoding/json"
	"errors"
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

// go test -timeout 200s -run ^TestBlacklistPathNextHopIsNotProxyVerifyFail$ github.com/KelvinWu602/forus-core/p2p -v -count=1
func TestBlacklistPathNextHopIsNotProxyVerifyFail(t *testing.T) {
	// Defined in test/config-TestBlacklistPath-NextHopIsNotProxy-VerifyFail-N1.yaml
	// Defined in test/config-TestBlacklistPath-NextHopIsNotProxy-VerifyFail-N2.yaml
	// Defined in test/config-TestBlacklistPath-NextHopIsNotProxy-VerifyFail-N3.yaml
	// N1 127.0.0.1
	// N2 127.0.0.2
	// N3 127.0.0.3

	assert := assert.New(t)

	os.Setenv("NUMBER_OF_COVER_NODES_FOR_PUBLISH", "0")
	os.Setenv("TARGET_NUMBER_OF_CONNECTED_PATHS", "0")

	var setup sync.WaitGroup
	setup.Add(4)
	// All component prefixed with 1 ==> Mock
	// All component prefixed with 2 ==> Normal
	members := map[string][]string{}
	members["127.0.0.1"] = []string{}
	members["127.0.0.2"] = []string{}
	members["127.0.0.3"] = []string{}
	NDlock := sync.RWMutex{}
	mockND1 := NewMockND("127.0.0.1", members, &NDlock)
	mockND2 := NewMockND("127.0.0.2", members, &NDlock)
	mockND3 := NewMockND("127.0.0.3", members, &NDlock)

	// Disconnected IS to simulate publish fail
	mockIS1 := NewMockIS()
	mockIS2 := NewMockIS()
	mockIS3 := NewMockIS()

	var N1, N2, N3 *Node
	go func() {
		initMockND(mockND1, "127.0.0.1:3200")
		initMockND(mockND2, "127.0.0.2:3200")
		initMockND(mockND3, "127.0.0.3:3200")

		initMockIS(mockIS1, "127.0.0.1:3100")
		initMockIS(mockIS2, "127.0.0.2:3100")
		initMockIS(mockIS3, "127.0.0.3:3100")

		setup.Done()
	}()

	go func() {
		N1 = StartNodeInternal("../unit_test_configs/config-TestBlacklistPath-NextHopIsNotProxy-VerifyFail-N1.yaml")
		setup.Done()
	}()

	go func() {
		N2 = StartNodeInternal("../unit_test_configs/config-TestBlacklistPath-NextHopIsNotProxy-VerifyFail-N2.yaml")
		setup.Done()
	}()

	go func() {
		N3 = StartNodeInternal("../unit_test_configs/config-TestBlacklistPath-NextHopIsNotProxy-VerifyFail-N3.yaml")
		setup.Done()
	}()
	setup.Wait()

	// http calls

	// post path on N1
	assert.Equal(0, N1.paths.getSize(), "N1 should have 0 paths at the beginning")
	postpathres, resBody := sendPostRequest("http://127.0.0.1:3000/path", `{}`)
	defer postpathres.Body.Close()
	if postpathres.StatusCode != http.StatusCreated {
		t.Fatal(postpathres.StatusCode, postpathres.Status, resBody, "N1:POST /path: wrong status code")
	}
	t.Log(resBody)
	assert.Equal(1, N1.paths.getSize(), "N1 should have 1 paths after post /path")
	// need to get the pathID
	var newPathID uuid.UUID
	N1.paths.iterate(func(pathId uuid.UUID, pathProfile PathProfile) {
		t.Log("N1 path uuid is ", pathId)
		newPathID = pathId
	}, true)

	// post path on N2
	assert.Equal(0, N2.paths.getSize(), "N2 should have 0 paths at the beginning")
	body := fmt.Sprintf(`{"ip":"127.0.0.1","path_id":"%v"}`, newPathID.String())
	t.Log("post path on N2 body", body)
	postpathres2, resBody := sendPostRequest("http://127.0.0.2:3000/path", body)
	defer postpathres2.Body.Close()
	if postpathres2.StatusCode != http.StatusCreated {
		t.Fatal(postpathres2.StatusCode, postpathres2.Status, resBody, "N2:POST /path: wrong status code")
	}
	t.Log(resBody)
	assert.Equal(1, N2.paths.getSize(), "N2 should have 1 paths after post /path")
	assert.Equal(1, N1.paths.getSize(), "N1 should have 1 paths after post /path")
	assert.Equal(1, N1.covers.getSize(), "N1 should have 1 cover after post /path")
	pathOnN2, _ := N2.paths.getValue(newPathID)
	coverN2Profile, found := N1.covers.getValue("127.0.0.2")
	assert.Equal(true, found, "should find N2 in N1's covers")
	assert.Equal(coverN2Profile.treeUUID, pathOnN2.uuid, "N1 path and N2 path should have the same tree id")

	// post path on N3
	assert.Equal(0, N3.paths.getSize(), "N3 should have 0 paths at the beginning")
	body = fmt.Sprintf(`{"ip":"127.0.0.2","path_id":"%v"}`, newPathID.String())
	t.Log("post path on N3 body", body)
	postpathres3, resBody := sendPostRequest("http://127.0.0.3:3000/path", body)
	defer postpathres3.Body.Close()
	if postpathres3.StatusCode != http.StatusCreated {
		t.Fatal(postpathres3.StatusCode, postpathres3.Status, resBody, "N3:POST /path: wrong status code")
	}
	t.Log(resBody)
	assert.Equal(1, N3.paths.getSize(), "N3 should have 1 paths after post /path")
	assert.Equal(1, N2.paths.getSize(), "N2 should have 1 paths after post /path")
	assert.Equal(1, N1.paths.getSize(), "N1 should have 1 paths after post /path")
	assert.Equal(0, N3.covers.getSize(), "N3 should have 0 cover after post /path")
	assert.Equal(1, N2.covers.getSize(), "N2 should have 1 cover after post /path")
	assert.Equal(1, N1.covers.getSize(), "N1 should have 1 cover after post /path")
	pathOnN3, _ := N3.paths.getValue(newPathID)
	coverN3Profile, found := N2.covers.getValue("127.0.0.3")
	assert.Equal(true, found, "should find N3 in N2's covers")
	assert.Equal(coverN3Profile.treeUUID, pathOnN3.uuid, "should have the same tree id")

	// terminate handleApplicationMessageWorker for this path on N1, to simulate later verify cover request being NOT VERIFIED
	coverN2Profile.cancelFunc(errors.New("simulate N2 move up now"))
	time.Sleep(time.Second)
	assert.Equal(0, N1.covers.getSize(), "N1 should have no covers now")
	assert.Equal(1, N2.paths.getSize(), "N2 should have n paths now")

	// // post message on N3
	body = fmt.Sprintf(`{"content": "%v"}`, testmessage)
	postmsgres, resBody := sendPostRequest(fmt.Sprintf("http://127.0.0.3:3000/message/%v", testkey), body)
	defer postmsgres.Body.Close()
	if postmsgres.StatusCode != http.StatusCreated {
		t.Fatal(postmsgres.StatusCode, postmsgres.Status, resBody, "post msg on N3 wrong status code")
	}
	t.Log(resBody)

	var res = HTTPPostMessageResp{}
	err := json.Unmarshal([]byte(resBody), &res)
	assert.Equal(nil, err, "should have no unmarshal error")

	// // wait for at least PUBLISH_JOB_FAILED_TIMEOUT: 3s
	time.Sleep(5 * time.Second)
	publishJobProfile, found := N3.publishJobs.getValue(res.PublishJobId)
	assert.Equal(true, found, "should have the publish profile")
	assert.Equal(TIMEOUT, publishJobProfile.Status, "should have status timeout")

	// MoveUp should be triggered! expect the only path is a proxy path
	// Wait for at least MAINTAIN_PATHS_HEALTH_CHECKING_INTERVAL + COVER_MESSAGE_SENDING_INTERVAL = 5s + 1s
	time.Sleep(8 * time.Second)

	N3.paths.iterate(func(pathID uuid.UUID, pathProfile PathProfile) {
		assert.NotEqual(newPathID, pathID, "N3 should have removed the old path")
		assert.Equal("ImmutableStorage", pathProfile.next, "N3 should only have proxy path")
		assert.Equal("", pathProfile.next2, "N3 should only have proxy path")
	}, true)

}
