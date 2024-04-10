package p2p

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
)

var testkey = "n12BW68CQf443flTP8YIAE6r8Ah1oGmevAQW46_T5PuHjvzgVgn08cboOOjHTz1s"
var testmessage = "n12BW68CQf443flTP8YIAE6r8Ah1oGmevAQW46/T5PuHjvzgVgn08cboOOjHTz1sewoJCSJoZWxsbyI6ICIxIiwKCQkieW9vb28iOiAyCgl9"
var testmessagepayload = "ewoJCSJoZWxsbyI6ICIxIiwKCQkieW9vb28iOiAyCgl9"

func sendPostRequest(url string, jsonBody string) (*http.Response, string) {
	body := []byte(jsonBody)
	resp, err := http.Post(url, "application/json", bytes.NewBuffer(body))
	if err != nil {
		log.Fatal(err)
	}
	bodyBytes, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Fatal("Error:", err)
		return resp, ""
	}

	bodyString := string(bodyBytes)
	return resp, bodyString
}

func sendGetRequest(url string) (*http.Response, string) {
	resp, err := http.Get(url)
	if err != nil {
		log.Fatal(err)
	}
	bodyBytes, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Fatal("Error:", err)
		return resp, ""
	}

	bodyString := string(bodyBytes)
	return resp, bodyString
}

// go test -timeout 30s -run ^TestConfigs$ github.com/KelvinWu602/forus-core/p2p -v -count=1
func TestConfigs(t *testing.T) {
	os.Setenv("NUMBER_OF_COVER_NODES_FOR_PUBLISH", "0")
	os.Setenv("TARGET_NUMBER_OF_CONNECTED_PATHS", "0")
	os.Setenv("GIN_MODE", "release")

	var setup sync.WaitGroup
	setup.Add(3)
	members := map[string][]string{}
	members["127.0.0.1"] = []string{}
	mockND := NewMockND("127.0.0.1", members, &sync.RWMutex{})
	mockIS := NewMockIS()
	go func() {
		initMockND(mockND, "3200")
		setup.Done()
	}()
	go func() {
		initMockIS(mockIS, "3100")
		setup.Done()
	}()
	go func() {
		StartNodeInternal("../unit_test_configs/config-TestPublishOnProxy.yaml")
		setup.Done()
	}()
	setup.Wait()

	// http calls
	// get configs
	res, resBody := sendGetRequest("http://localhost:3000/configs")
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatal(res.StatusCode, res.Status, resBody, "get configs wrong status code")
	}
	t.Log(resBody)
}

func TestMockND(t *testing.T) {
	assert := assert.New(t)
	var setup sync.WaitGroup
	setup.Add(1)
	// All component prefixed with 1 ==> Mock
	// All component prefixed with 2 ==> Normal
	members := map[string][]string{}
	members["127.0.0.1:3001"] = []string{}
	members["127.0.0.1:4001"] = []string{}
	NDlock := sync.RWMutex{}
	mockND1 := NewMockND("127.0.0.1:4001", members, &NDlock)
	mockND2 := NewMockND("127.0.0.1:3001", members, &NDlock)

	go func() {
		initMockND(mockND1, "3201")
		initMockND(mockND2, "3200")
		setup.Done()
	}()
	setup.Wait()

	v1 := viper.New()
	v2 := viper.New()

	v1.SetDefault("NODE_DISCOVERY_SERVER_LISTEN_PORT", ":3201")
	v2.SetDefault("NODE_DISCOVERY_SERVER_LISTEN_PORT", ":3200")
	ndclient1 := initNodeDiscoverClient(v1)
	ndclient2 := initNodeDiscoverClient(v2)

	resp1, err1 := ndclient1.GetMembers()
	resp2, err2 := ndclient2.GetMembers()
	assert.Equal(nil, err1, "should be nil")
	assert.Equal(nil, err2, "should be nil")
	assert.Equal([]string(nil), resp1.Member, "should be nil")
	assert.Equal([]string(nil), resp2.Member, "should be nil")

	// ND1 joinCluster
	_, err1 = ndclient1.JoinCluster("127.0.0.1:3001")
	assert.Equal(nil, err1, "should be nil")

	resp1, err1 = ndclient1.GetMembers()
	resp2, err2 = ndclient2.GetMembers()
	assert.Equal(nil, err1, "should be nil")
	assert.Equal(nil, err2, "should be nil")
	assert.Equal([]string{"127.0.0.1:3001"}, resp1.Member, "should be nil")
	assert.Equal([]string{"127.0.0.1:4001"}, resp2.Member, "should be nil")

	// ND1 LeaveCluster
	_, err1 = ndclient1.LeaveCluster()
	assert.Equal(nil, err1, "should be nil")

	resp1, err1 = ndclient1.GetMembers()
	resp2, err2 = ndclient2.GetMembers()
	assert.Equal(nil, err1, "should be nil")
	assert.Equal(nil, err2, "should be nil")
	assert.Equal([]string(nil), resp1.Member, "should be nil")
	assert.Equal([]string(nil), resp2.Member, "should be nil")
}

// go test -timeout 30s -run ^TestPublishOnProxy$ github.com/KelvinWu602/forus-core/p2p -v -count=1
func TestPublishOnProxy(t *testing.T) {
	// NormalNode.Publish

	os.Setenv("NUMBER_OF_COVER_NODES_FOR_PUBLISH", "0")
	os.Setenv("TARGET_NUMBER_OF_CONNECTED_PATHS", "0")

	var setup sync.WaitGroup
	setup.Add(3)
	members := map[string][]string{}
	members["127.0.0.1"] = []string{}
	mockND := NewMockND("127.0.0.1", members, &sync.RWMutex{})
	mockIS := NewMockIS()
	go func() {
		initMockND(mockND, "3200")
		setup.Done()
	}()
	go func() {
		initMockIS(mockIS, "3100")
		setup.Done()
	}()
	go func() {
		StartNodeInternal("../unit_test_configs/config-TestPublishOnProxy.yaml")
		setup.Done()
	}()
	setup.Wait()

	// http calls

	// post path
	postpathres, resBody := sendPostRequest("http://localhost:3000/path", `{}`)
	defer postpathres.Body.Close()
	if postpathres.StatusCode != http.StatusOK {
		t.Fatal(postpathres.StatusCode, postpathres.Status, resBody, "post path wrong status code")
	}
	t.Log(resBody)

	// post message
	body := fmt.Sprintf(`{"content": "%v"}`, testmessage)
	postmsgres, resBody := sendPostRequest(fmt.Sprintf("http://localhost:3000/message/%v", testkey), body)
	defer postmsgres.Body.Close()
	if postmsgres.StatusCode != http.StatusCreated {
		t.Fatal(postmsgres.StatusCode, postmsgres.Status, resBody, "post msg wrong status code")
	}
	t.Log(resBody)

	// get message
	getmsgres, resBody := sendGetRequest(fmt.Sprintf("http://localhost:3000/message/%v", testkey))
	defer getmsgres.Body.Close()
	if getmsgres.StatusCode != http.StatusOK {
		t.Fatal(getmsgres.StatusCode, getmsgres.Status, resBody, "get message wrong status code")
	}
	t.Log(resBody)
}

// go test -timeout 120s -run ^TestPublishOnCover$ github.com/KelvinWu602/forus-core/p2p -v -count=1
func TestPublishOnCover(t *testing.T) {
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
	if postpathres.StatusCode != http.StatusOK {
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
	if postpathres.StatusCode != http.StatusOK {
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

	// post message on cover
	body = fmt.Sprintf(`{"content": "%v"}`, testmessage)
	postmsgres, resBody := sendPostRequest(fmt.Sprintf("http://127.0.0.2:3000/message/%v", testkey), body)
	defer postmsgres.Body.Close()
	if postmsgres.StatusCode != http.StatusCreated {
		t.Fatal(postmsgres.StatusCode, postmsgres.Status, resBody, "post msg on cover wrong status code")
	}
	t.Log(resBody)

	// wait for forward
	time.Sleep(3 * time.Second)

	// get message
	getmsgres, resBody := sendGetRequest(fmt.Sprintf("http://127.0.0.2:3000/message/%v", testkey))
	defer getmsgres.Body.Close()
	if getmsgres.StatusCode != http.StatusOK {
		t.Fatal(getmsgres.StatusCode, getmsgres.Status, resBody, "get message wrong status code")
	}
	t.Log(resBody)

	msg := HTTPSchemaMessage{}
	err := json.Unmarshal([]byte(resBody), &msg)
	base64encodedContent := base64.StdEncoding.EncodeToString(msg.Content)
	assert.Equal(nil, err, "should have no unmarshal error")
	assert.Equal(testmessagepayload, base64encodedContent, "content mismatch")
}

// go test -timeout 240s -run ^TestForwardOnCover$ github.com/KelvinWu602/forus-core/p2p -v -count=1
func TestForwardOnCover(t *testing.T) {
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

	mockIS1 := NewMockIS()
	mockIS2 := ShareCacheWith(mockIS1)
	mockIS3 := ShareCacheWith(mockIS1)

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
		N1 = StartNodeInternal("../unit_test_configs/config-TestForwardOnCover-N1.yaml")
		setup.Done()
	}()

	go func() {
		N2 = StartNodeInternal("../unit_test_configs/config-TestForwardOnCover-N2.yaml")
		setup.Done()
	}()

	go func() {
		N3 = StartNodeInternal("../unit_test_configs/config-TestForwardOnCover-N3.yaml")
		setup.Done()
	}()
	setup.Wait()

	// http calls

	// post path on N1
	assert.Equal(0, N1.paths.getSize(), "N1 should have 0 paths at the beginning")
	postpathres, resBody := sendPostRequest("http://127.0.0.1:3000/path", `{}`)
	defer postpathres.Body.Close()
	if postpathres.StatusCode != http.StatusOK {
		t.Fatal(postpathres.StatusCode, postpathres.Status, resBody, "N1:POST /path: wrong status code")
	}
	t.Log(resBody)
	assert.Equal(1, N1.paths.getSize(), "N1 should have 1 paths after post /path")
	// need to get the pathID
	var newPathID uuid.UUID
	N1.paths.iterate(func(u uuid.UUID, _ PathProfile) {
		newPathID = u
	}, true)

	// post path on N2
	assert.Equal(0, N2.paths.getSize(), "N2 should have 0 paths at the beginning")
	body := fmt.Sprintf(`{"ip":"127.0.0.2","path_id":"%v"}`, newPathID.String())
	t.Log("post path on N2 body", body)
	postpathres2, resBody := sendPostRequest("http://127.0.0.2:3000/path", body)
	defer postpathres2.Body.Close()
	if postpathres2.StatusCode != http.StatusOK {
		t.Fatal(postpathres2.StatusCode, postpathres2.Status, resBody, "N2:POST /path: wrong status code")
	}
	t.Log(resBody)
	assert.Equal(1, N2.paths.getSize(), "N2 should have 1 paths after post /path")
	assert.Equal(1, N1.paths.getSize(), "N1 should have 1 paths after post /path")
	assert.Equal(1, N1.covers.getSize(), "N1 should have 1 cover after post /path")
	pathOnN2, _ := N2.paths.getValue(newPathID)
	N1.covers.iterate(func(coverIP string, coverProfile CoverNodeProfile) {
		t.Log("Proxy covers:", coverIP, coverProfile)
		assert.Equal(coverProfile.treeUUID, pathOnN2.uuid, "N1 path and N2 path should have the same tree id")
	}, true)

	// post path on N3
	assert.Equal(0, N3.paths.getSize(), "N3 should have 0 paths at the beginning")
	body = fmt.Sprintf(`{"ip":"127.0.0.3","path_id":"%v"}`, newPathID.String())
	t.Log("post path on N3 body", body)
	postpathres3, resBody := sendPostRequest("http://127.0.0.3:3000/path", body)
	defer postpathres3.Body.Close()
	if postpathres3.StatusCode != http.StatusOK {
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
	N2.covers.iterate(func(coverIP string, coverProfile CoverNodeProfile) {
		t.Log("Proxy covers:", coverIP, coverProfile)
		assert.Equal(coverProfile.treeUUID, pathOnN3.uuid, "should have the same tree id")
	}, true)

	// post message on N3
	body = fmt.Sprintf(`{"content": "%v"}`, testmessage)
	postmsgres, resBody := sendPostRequest(fmt.Sprintf("http://127.0.0.3:3000/message/%v", testkey), body)
	defer postmsgres.Body.Close()
	if postmsgres.StatusCode != http.StatusCreated {
		t.Fatal(postmsgres.StatusCode, postmsgres.Status, resBody, "post msg on N3 wrong status code")
	}
	t.Log(resBody)

	// wait for forward
	time.Sleep(5 * time.Second)

	// get message on N3
	getmsgres, resBody := sendGetRequest(fmt.Sprintf("http://127.0.0.3:3000/message/%v", testkey))
	defer getmsgres.Body.Close()
	if getmsgres.StatusCode != http.StatusOK {
		t.Fatal(getmsgres.StatusCode, getmsgres.Status, resBody, "get message wrong status code")
	}
	t.Log(resBody)

	msg := HTTPSchemaMessage{}
	err := json.Unmarshal([]byte(resBody), &msg)
	base64encodedContent := base64.StdEncoding.EncodeToString(msg.Content)
	assert.Equal(nil, err, "should have no unmarshal error")
	assert.Equal(testmessagepayload, base64encodedContent, "content mismatch")
}
