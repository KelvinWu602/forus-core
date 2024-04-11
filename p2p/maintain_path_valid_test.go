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

// path with unreachable next hop (simulated by terminating first node, not ready for now)
// path with not sync next next hop or proxy public key (simulated by changing first node)
// path not exists in next hop (simulated by removing in first node)

// go test -timeout 120s -run ^TestInvalidPathOutOfSync$ github.com/KelvinWu602/forus-core/p2p -v -count=1
func TestInvalidPathOutOfSync(t *testing.T) {
	assert := assert.New(t)
	// Proxy (127.0.0.1)
	// Cover (127.0.0.2)

	os.Setenv("NUMBER_OF_COVER_NODES_FOR_PUBLISH", "0")
	os.Setenv("TARGET_NUMBER_OF_CONNECTED_PATHS", "0")

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
	mockIS2 := ShareCacheWith(mockIS1)

	var proxy, cover *Node

	go func() {
		initMockND(mockND1, "127.0.0.1:3200")
		initMockND(mockND2, "127.0.0.2:3200")
		initMockIS(mockIS1, "127.0.0.1:3100")
		initMockIS(mockIS2, "127.0.0.2:3100")
		setup.Done()
	}()

	go func() {
		proxy = StartNodeInternal("../unit_test_configs/config-TestInvalidPath-OutOfSync-Parent.yaml")
		setup.Done()
	}()

	go func() {
		cover = StartNodeInternal("../unit_test_configs/config-TestInvalidPath-OutOfSync-Cover.yaml")
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
	proxy.covers.iterate(func(coverIP string, coverProfile CoverNodeProfile) {
		t.Log("Proxy covers:", coverIP, coverProfile)
		assert.Equal(coverProfile.treeUUID, pathOnCover.uuid, "should have the same tree id")
	}, true)

	// path with not sync next next hop or proxy public key (simulated by changing first node's path profile data directly)
	proxyPath, found := proxy.paths.getValue(newPathID)
	assert.Equal(true, found, "proxy should have a self proxy path")
	proxyPath.next = "127.0.0.3"  // non-existing IP
	proxyPath.next2 = "127.0.0.4" // non-existing IP
	proxy.paths.setValue(newPathID, proxyPath)

	// wait for MAINTAIN_PATHS_HEALTH_CHECKING_INTERVAL: 5s
	time.Sleep(10 * time.Second)

	// should see this path on cover fixed
	coverPath, found := cover.paths.getValue(newPathID)
	assert.Equal(true, found, "cover should have a path")
	assert.Equal("127.0.0.3", coverPath.next2, "should be sync as parent")
}

// go test -timeout 120s -run ^TestInvalidPathOutOfSync2$ github.com/KelvinWu602/forus-core/p2p -v -count=1
func TestInvalidPathOutOfSync2(t *testing.T) {
	assert := assert.New(t)
	// Proxy (127.0.0.1)
	// Cover (127.0.0.2)

	os.Setenv("NUMBER_OF_COVER_NODES_FOR_PUBLISH", "0")
	os.Setenv("TARGET_NUMBER_OF_CONNECTED_PATHS", "0")

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
	mockIS2 := ShareCacheWith(mockIS1)

	var proxy, cover *Node

	go func() {
		initMockND(mockND1, "127.0.0.1:3200")
		initMockND(mockND2, "127.0.0.2:3200")
		initMockIS(mockIS1, "127.0.0.1:3100")
		initMockIS(mockIS2, "127.0.0.2:3100")
		setup.Done()
	}()

	go func() {
		proxy = StartNodeInternal("../unit_test_configs/config-TestInvalidPath-OutOfSync2-Parent.yaml")
		setup.Done()
	}()

	go func() {
		cover = StartNodeInternal("../unit_test_configs/config-TestInvalidPath-OutOfSync2-Cover.yaml")
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
	proxy.covers.iterate(func(coverIP string, coverProfile CoverNodeProfile) {
		t.Log("Proxy covers:", coverIP, coverProfile)
		assert.Equal(coverProfile.treeUUID, pathOnCover.uuid, "should have the same tree id")
	}, true)

	// path not exists on next hop (simulated by changing first node's path profile data directly)
	proxy.paths.deleteValue(newPathID)

	// wait for MAINTAIN_PATHS_HEALTH_CHECKING_INTERVAL: 5s
	time.Sleep(10 * time.Second)

	// should not see this path on cover fixed
	_, found := cover.paths.getValue(newPathID)
	assert.Equal(false, found, "cover should not have a path")
	t.Log(cover.paths.data)
}
