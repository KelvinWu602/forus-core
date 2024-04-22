package p2p

import (
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

// go test -timeout 120s -run ^TestParentCrash$ github.com/KelvinWu602/forus-core/p2p -v -count=1
func TestParentCrash(t *testing.T) {
	assert := assert.New(t)
	// Proxy (127.0.0.1)
	// Cover (127.0.0.2)

	// disable nodes auto connect paths
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
		proxy = StartNodeInternal("../unit_test_configs/config-TestPublishOnCover-Proxy.yaml")
		setup.Done()
	}()

	go func() {
		cover = StartNodeInternal("../unit_test_configs/config-TestPublishOnCover-Cover.yaml")
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

	// the parent crash can be simulated as terminating the its corresponding handleApplicationMessageWorker
	coverProfile.cancelFunc(errors.New("test for sudden termination"))

	// wait for the connection to terminate
	time.Sleep(3 * time.Second)

	// should not see this path on cover
	assert.Equal(0, cover.paths.getSize(), "should not contain any paths")
}

// go test -timeout 120s -run ^TestCoverCrash$ github.com/KelvinWu602/forus-core/p2p -v -count=1
func TestCoverCrash(t *testing.T) {
	assert := assert.New(t)
	// Proxy (127.0.0.1)
	// Cover (127.0.0.2)

	// disable nodes auto connect paths
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
		proxy = StartNodeInternal("../unit_test_configs/config-TestPublishOnCover-Proxy.yaml")
		setup.Done()
	}()

	go func() {
		cover = StartNodeInternal("../unit_test_configs/config-TestPublishOnCover-Cover.yaml")
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

	// the cover crash can be simulated as terminating the its corresponding sendCoverMessageWorker
	pathOnCover.cancelFunc(errors.New("test for sudden termination"))

	// wait for the connection to terminate
	time.Sleep(3 * time.Second)

	// should not see this cover on parent
	assert.Equal(0, proxy.covers.getSize(), "should not contain any covers")
}

// go test -timeout 120s -run ^TestCoverTimeout$ github.com/KelvinWu602/forus-core/p2p -v -count=1
func TestCoverTimeout(t *testing.T) {
	assert := assert.New(t)
	// Proxy (127.0.0.1)
	// Cover (127.0.0.2)

	// disable nodes auto connect paths
	os.Setenv("TARGET_NUMBER_OF_CONNECTED_PATHS", "0")

	// Defined in test/config-TestCoverTimeout-Cover.yaml
	// COVER_MESSAGE_SENDING_INTERVAL: 60s

	// Defined in test/config-TestCoverTimeout-Proxy.yaml
	// APPLICATION_MESSAGE_RECEIVING_INTERVAL: 5s

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
		proxy = StartNodeInternal("../unit_test_configs/config-TestCoverTimeout-Proxy.yaml")
		setup.Done()
	}()

	go func() {
		cover = StartNodeInternal("../unit_test_configs/config-TestCoverTimeout-Cover.yaml")
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

	// Wait for at least APPLICATION_MESSAGE_RECEIVING_INTERVAL = 5s
	time.Sleep(6 * time.Second)

	// should not see this cover on parent

	proxy.covers.iterate(func(ip string, coverprofile CoverNodeProfile) {
		t.Log(ip, coverprofile)
	}, true)

	assert.Equal(0, proxy.covers.getSize(), "should not contain any covers")
}
