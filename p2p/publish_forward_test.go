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

func TestConfigs(t *testing.T) {
	// NormalNode.Publish

	os.Setenv("NUMBER_OF_COVER_NODES_FOR_PUBLISH", "0")
	os.Setenv("TARGET_NUMBER_OF_CONNECTED_PATHS", "0")

	var setup sync.WaitGroup
	setup.Add(3)
	mockND := NewMockND([]string{})
	mockIS := NewMockIS()
	go func() {
		initMockND(mockND, "3201")
		setup.Done()
	}()
	go func() {
		initMockIS(mockIS, "3101")
		setup.Done()
	}()
	go func() {
		StartNodeInternal("../unit_test_configs/config-TestPublishOnCover-Proxy.yaml")
		setup.Done()
	}()
	setup.Wait()

	// http calls
	// get configs
	res, resBody := sendGetRequest("http://localhost:4000/configs")
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatal(res.StatusCode, res.Status, resBody, "get configs wrong status code")
	}
	t.Log(resBody)
}

// go test -timeout 30s -run ^TestPublishOnProxy$ github.com/KelvinWu602/forus-core/p2p -v -count=1
func TestPublishOnProxy(t *testing.T) {
	// NormalNode.Publish

	os.Setenv("NUMBER_OF_COVER_NODES_FOR_PUBLISH", "0")
	os.Setenv("TARGET_NUMBER_OF_CONNECTED_PATHS", "0")

	var setup sync.WaitGroup
	setup.Add(3)
	mockND := NewMockND([]string{})
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
	// MockProxy (4000,4001) <- NormalNode.Publish (3000,3001)
	// MockProxy.IS (3101) <==> NormalNode.IS (3100)
	// MockProxy.ND (3201) <==> NormalNode.ND (3200)

	os.Setenv("NUMBER_OF_COVER_NODES_FOR_PUBLISH", "0")
	os.Setenv("TARGET_NUMBER_OF_CONNECTED_PATHS", "0")

	var setup sync.WaitGroup
	setup.Add(3)
	// All component prefixed with 1 ==> Mock
	// All component prefixed with 2 ==> Normal
	mockND1 := NewMockND([]string{"127.0.0.1:3001"})
	mockND2 := NewMockND([]string{"127.0.0.1:4001"})

	mockIS1 := NewMockIS()
	mockIS2 := ShareCacheWith(mockIS1)

	var proxy, cover *Node

	go func() {
		initMockND(mockND1, "3201")
		initMockIS(mockIS1, "3101")
		initMockND(mockND2, "3200")
		initMockIS(mockIS2, "3100")
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
	postpathres, resBody := sendPostRequest("http://localhost:4000/path", `{}`)
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
	body := fmt.Sprintf(`{"ip":"127.0.0.1","port":"4001","path_id":"%v"}`, newPathID.String())
	t.Log("post path on cover body", body)
	postpathres2, resBody := sendPostRequest("http://localhost:3000/path", body)
	defer postpathres2.Body.Close()
	if postpathres.StatusCode != http.StatusOK {
		t.Fatal(postpathres.StatusCode, postpathres.Status, resBody, "post path on cover wrong status code")
	}
	t.Log(resBody)
	assert.Equal(1, cover.paths.getSize(), "cover should have 1 paths after post /path")
	assert.Equal(1, proxy.covers.getSize(), "proxy should have 1 cover after post /path")

	// post message on cover
	body = fmt.Sprintf(`{"content": "%v"}`, testmessage)
	postmsgres, resBody := sendPostRequest(fmt.Sprintf("http://localhost:3000/message/%v", testkey), body)
	defer postmsgres.Body.Close()
	if postmsgres.StatusCode != http.StatusCreated {
		t.Fatal(postmsgres.StatusCode, postmsgres.Status, resBody, "post msg on cover wrong status code")
	}
	t.Log(resBody)

	// wait for forward
	time.Sleep(3 * time.Second)

	// get message
	getmsgres, resBody := sendGetRequest(fmt.Sprintf("http://localhost:3000/message/%v", testkey))
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

func TestForwardOnCover(t *testing.T) {
	// MockProxy <- NormalNode.Forward <- MockCover.Publish
}
