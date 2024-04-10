package p2p

import (
	"bytes"
	"context"
	"encoding/gob"
	"errors"
	"fmt"
	"math/rand"
	"net"
	"sync"
	"time"

	"golang.org/x/exp/slices"

	"github.com/google/uuid"
)

type PublishJobStatus uint

const (
	PENDING PublishJobStatus = iota
	SUCCESS
	TIMEOUT
)

type PublishJobProfile struct {
	Key    []byte
	Status PublishJobStatus
	OnPath uuid.UUID
}

type InvalidPathProfile struct {
	SelfProfile    PathProfile
	NextHopProfile Path
	HandleType     HandleType
}

type HandleType uint

const (
	CLEAN HandleType = iota
	FIX
)

// helper functions
// ==========================================

func (node *Node) markPublishJobStatus(jobID uuid.UUID, job PublishJobProfile) {
	node.publishJobs.setValue(jobID, job)
	node.paths.lock.Lock()
	defer node.paths.lock.Unlock()
	if path, found := node.paths.data[job.OnPath]; found {
		if job.Status == SUCCESS {
			path.successCount++
		} else {
			path.failureCount++
		}
		node.paths.data[job.OnPath] = path
	}
}

func (node *Node) blacklistPathIDs() []uuid.UUID {
	// for every path, check if FailureCount > failureThreshold
	results := []uuid.UUID{}
	node.paths.iterate(func(pathID uuid.UUID, path PathProfile) {
		if path.failureCount >= node.v.GetInt("MOVE_UP_REQUIREMENT_FAILURE_THRESHOLD") {
			logMsg(node.name, "blacklistPathIDs", fmt.Sprintf("Blacklist Path %v: failureCount = %v > %v = threshold", pathID, path.failureCount, node.v.GetInt("MOVE_UP_REQUIREMENT_FAILURE_THRESHOLD")))
			results = append(results, pathID)
		}
	}, false)

	return results
}

func (node *Node) invalidPathProfiles() []InvalidPathProfile {
	// passive check
	// compare local path profile with QueryPath responses from path's next-hop
	// Clean the local path if:
	// a) next-hop failed to respond to QueryPath
	// b) next-hop does not have a path with the same pathID
	// Fix the local path if:
	// next hop has a path with same pathID but self.next_next != resp.next or self.proxyPublic != resp.proxyPublic

	results := []InvalidPathProfile{}

	node.paths.iterate(func(pathID uuid.UUID, oldPath PathProfile) {
		// proxy path is always true
		if oldPath.next == "ImmutableStorage" {
			return
		}
		nextHopIp := oldPath.next
		resp, err := node.sendQueryPathRequest(nextHopIp)
		if err != nil {
			// next-hop is unreachable, should clean this path
			logMsg(node.name, "invalidPathProfiles", fmt.Sprintf("Invlid Path %v: next-hop = %v is unreachable", pathID, nextHopIp))
			results = append(results, InvalidPathProfile{SelfProfile: oldPath, HandleType: CLEAN})
			return
		}

		for _, newPath := range resp.Paths {
			newPathID, err := DecryptUUID(newPath.EncryptedTreeUUID, node.privateKey)
			if err == nil && newPathID == pathID {
				if oldPath.next2 != newPath.NextHopIP || !slices.Equal(oldPath.proxyPublic, newPath.ProxyPublicKey) {
					logMsg(node.name, "invalidPathProfiles", fmt.Sprintf("Invlid Path %v: inconsistent path data:\nlocal:%v\nresp:%v\n", pathID, oldPath, newPath))
					// inconsistent path data, should fix local path
					results = append(results, InvalidPathProfile{SelfProfile: oldPath, NextHopProfile: newPath, HandleType: FIX})
					return
				}
				// valid path
				return
			}
		}

		// cannot find this path on next-hop, should clean this path
		logMsg(node.name, "invalidPathProfiles", fmt.Sprintf("Invlid Path %v: path not found on next-hop = %v", pathID, nextHopIp))
		results = append(results, InvalidPathProfile{SelfProfile: oldPath, HandleType: CLEAN})
	}, true)

	return results
}

func (node *Node) needMorePaths() bool {
	return node.paths.getSize() < node.v.GetInt("TARGET_NUMBER_OF_CONNECTED_PATHS")
}

func (node *Node) canPublish(lock bool) bool {
	if lock {
		return node.covers.getSize() >= node.v.GetInt("NUMBER_OF_COVER_NODES_FOR_PUBLISH") &&
			node.paths.getSize() > 0
	}
	return len(node.covers.data) >= node.v.GetInt("NUMBER_OF_COVER_NODES_FOR_PUBLISH") &&
		len(node.paths.data) > 0
}

func (n *Node) handleApplicationMessage(rawMessage ApplicationMessage, coverProfile CoverNodeProfile, forwardPathProfile PathProfile) error {
	// 1. Symmetric Decrypt.
	coverIp := coverProfile.cover
	symmetricKey := coverProfile.secretKey
	symOutput := rawMessage.SymmetricEncryptedPayload
	symInputBytes, err := SymmetricDecrypt(symOutput, symmetricKey)
	if err != nil {
		logError(n.name, "handleApplicationMessage", err, fmt.Sprintf("symmetric decrypt failed, from cover = %v", coverIp))
		return err
	}
	// 2. Decode Symmetric Input Bytes
	symInput := SymmetricEncryptDataMessage{}
	if err := gob.NewDecoder(bytes.NewBuffer(symInputBytes)).Decode(&symInput); err != nil {
		logError(n.name, "handleApplicationMessage", err, fmt.Sprintf("symmetric decrypt failed, from cover = %v", coverIp))
		return err
	}
	// 3. Check Type
	switch symInput.Type {
	case Cover:
		// simply discarded. The timeout is cancelled when this node received an ApplicationMessage, regardless of Cover or Real.
		return nil
	case Real:
		logMsg(n.name, "handleApplicationMessage", fmt.Sprintf("Real Message Received, from cover = %v", coverIp))
		return n.handleRealMessage(symInput.AsymetricEncryptedPayload, coverProfile, forwardPathProfile)
	default:
		logError(n.name, "handleApplicationMessage", err, fmt.Sprintf("invalid message type, from cover = %v", coverIp))
		return errors.New("invalid data message type")
	}
}

func (n *Node) handleRealMessage(asymOutput []byte, coverProfile CoverNodeProfile, forwardPathProfile PathProfile) error {
	priKey := n.privateKey
	asymInputBytes, err := AsymmetricDecrypt(asymOutput, priKey)
	// Check if self is the proxy
	if err != nil && !errors.Is(err, errWrongPrivateKey) {
		// Failed to decrypt, unknown reason
		logError(n.name, "handleRealMessage", err, fmt.Sprintf("asymmetric decrypt failed, asymOutput = %v", asymOutput))
		return err
	}

	isProxy := err == nil
	coverIp := coverProfile.cover
	forwardPathId := forwardPathProfile.uuid
	if isProxy && forwardPathProfile.next != "ImmutableStorage" {
		// if the real message is asymmetrically encrypted with my private key, but routing info says this message should be routed elsewhere, ignore
		logMsg(n.name, "handleRealMessage", fmt.Sprintf("IGNORED valid real msg from cover %v connected to %v, next-hop = %v but AsymDecrypt OK", coverIp, forwardPathId, forwardPathProfile.next))
		return errors.New("can asym decrypt but next hop is not IS")
	}

	if isProxy {
		asymInput := AsymetricEncryptDataMessage{}
		err = gob.NewDecoder(bytes.NewBuffer(asymInputBytes)).Decode(&asymInput)
		if err != nil {
			logError(n.name, "handleRealMessage", err, fmt.Sprintf("asymmetric decode failed, asymInputBytes = %v", asymInputBytes))
			return err
		}
		// Store in ImmutableStorage
		key := asymInput.Data.Key
		content := asymInput.Data.Content
		resp, err := n.isClient.Store(key, content)
		if err != nil {
			logError(n.name, "handleRealMessage", err, fmt.Sprintf("store real message failed, key = %v, content = %v", key, content))
			return err
		}
		logMsg(n.name, "handleRealMessage", fmt.Sprintf("Store Real Message Success: Status = %v]:Key = %v", resp.Success, key))
	} else {
		// Call Forward
		err := n.Forward(asymOutput, coverProfile, forwardPathProfile)
		if err != nil {
			logError(n.name, "handleRealMessage", err, fmt.Sprintf("Forward failed on paths = %v, asymOutput = %v", forwardPathId, asymOutput))
			return err
		}
		logMsg(n.name, "handleRealMessage", fmt.Sprintf("Forward Real Message Success: from %v to %v", coverIp, forwardPathId))
	}
	return nil
}

// move up one step
func (n *Node) MoveUp(pathID uuid.UUID) {

	logMsg(n.name, "MoveUp", fmt.Sprintf("Starts, Path: %v", pathID.String()))

	value, ok := n.paths.getValue(pathID)
	if ok {
		originalNext := value.next
		originalNextNext := value.next2
		// 1) check what originalNextNext is
		if originalNextNext == "ImmutableStorage" {
			// next-next-hop is ImmutableStorage --> next-hop is proxy --> cannot move up, delete this blacklist path
			n.deletePathAndRelatedCovers(pathID)
			logMsg(n.name, "MoveUp", fmt.Sprintf("Ends, Path: %v:  Case: Next-Next is ImmutableStorage, path is removed", pathID.String()))
			return
		}
		// 2) send verifyCover(originalNext) to originNextNext
		resp, err := n.sendVerifyCoverRequest(originalNextNext, originalNext)
		if err != nil {
			// next-next-hop is unreachable --> cannot move up, delete this blacklist path
			n.deletePathAndRelatedCovers(pathID)
			logError(n.name, "MoveUp", err, fmt.Sprintf("Ends, Path: %v:  Case: error during verifyCover, path is removed", pathID.String()))
			return
		}
		if resp.IsVerified {
			// connectPath with originalNextNext, it will overwrite the current blacklist path
			resp, err := n.ConnectPath(originalNextNext, pathID)
			if err == nil && resp.Accepted {
				// if success, done
				logMsg(n.name, "MoveUp", fmt.Sprintf("Ends, Path: %v:  Case: Successfully connected to next next hop", pathID.String()))
				return
			}
		}

		// 3) if resp is not verified OR connectPath failed
		// self becomes proxy
		newPathProfile := PathProfile{
			uuid:         uuid.New(),
			next:         "ImmutableStorage",
			next2:        "",
			proxyPublic:  n.publicKey,
			successCount: 0,
			failureCount: 0,
		}
		// delete cover nodes that are connected to the old blacklist path
		n.deletePathAndRelatedCovers(pathID)
		n.paths.setValue(newPathProfile.uuid, newPathProfile)
	}

	logMsg(n.name, "MoveUp", fmt.Sprintf("Ends, Path: %v:  Case: Self becomes proxy success", pathID.String()))
}

// MoveUp helper: move cover nodes
func (n *Node) deletePathAndRelatedCovers(pathID uuid.UUID) {
	path, found := n.paths.getValue(pathID)
	if !found {
		return
	}
	n.covers.iterate(func(coverIP string, coverProfile CoverNodeProfile) {
		if coverProfile.treeUUID == pathID {
			// this will terminate the corresponding handle application msg worker, which will in turn clean the cover profile
			n.covers.data[coverIP].cancelFunc(errors.New("deleted during move up"))
		}
	}, false)
	path.cancelFunc(errors.New("deleted during move up"))
}

// Tree Formation Process by aggregating QueryPath, CreateProxy, ConnectPath & joining cluster
func (n *Node) getMorePaths() {

	logMsg(n.name, "fulfillPublishCondition", "Started")

	// Get all cluster members IP
	resp, err := n.ndClient.GetMembers()
	if err != nil {
		logError(n.name, "fulfillPublishCondition", err, "Get Member Error. Iteration Skip")
		return
	}
	clusterSize := len(resp.Member)
	if clusterSize <= 0 {
		logMsg(n.name, "fulfillPublishCondition", "Get Members return empty array. Iteration Skip")
		return
	}

	timeout := time.After(n.v.GetDuration("FULFILL_PUBLISH_CONDITION_TIMEOUT"))

	for n.paths.getSize() < n.v.GetInt("TARGET_NUMBER_OF_CONNECTED_PATHS") {
		done := make(chan bool)
		go func() {
			peerChoice := rand.Intn(clusterSize)
			memberIP := resp.Member[peerChoice]
			logMsg(n.name, "fulfillPublishCondition", fmt.Sprintf("Connect %v", memberIP))
			_, pendingPaths, _ := n.QueryPath(memberIP)
			if len(pendingPaths) > 0 {
				// if the peer offers some path, connect to it
				// will not connect to all path under the same node to diverse risk
				pathChoice := rand.Intn(len(pendingPaths))
				if _, err := n.ConnectPath(memberIP, pendingPaths[pathChoice].uuid); err != nil {
					logError(n.name, "fulfillPublishCondition", err, fmt.Sprintf("ConnectPath Error: Connect %v", memberIP))
				}
			} else {
				// if the peer does not have path, ask it to become a proxy
				if _, err := n.CreateProxy(memberIP); err != nil {
					logError(n.name, "fulfillPublishCondition", err, fmt.Sprintf("CreateProxy Error: Connect %v", memberIP))
				}
			}
			done <- true
			defer close(done)
		}()
		select {
		case <-done:
			time.Sleep(n.v.GetDuration("FULFILL_PUBLISH_CONDITION_INTERVAL"))
			continue
		case <-timeout:
			logMsg(n.name, "fulfillPublishCondition", "Iteration Timeout")
			return
		}
	}
	logMsg(n.name, "fulfillPublishCondition", "Iteration Success")
}

// workers
// ==========================================

// publishJobStatusChecker will monitor the status of a publish job for a given timeout.
func (node *Node) checkPublishJobStatusWorker(jobID uuid.UUID) {

	logMsg(node.name, "checkPublishJobStatusWorker", fmt.Sprintf("Started, JobID: %v", jobID.String()))

	timeout := node.v.GetDuration("PUBLISH_JOB_FAILED_TIMEOUT")
	checkInterval := node.v.GetDuration("PUBLISH_JOB_CHECKING_INTERVAL")

	// check if the job really exists
	publishJobProfile, jobExists := node.publishJobs.getValue(jobID)
	if !jobExists {
		logMsg(node.name, "checkPublishJobStatusWorker", fmt.Sprintf("JobID %v does not exist", jobID.String()))
		return
	}

	// channel for the inner function to report success
	done := make(chan bool)
	defer close(done)
	ctx, cancel := context.WithCancel(context.Background())
	go func(ctx context.Context) {
		for {
			resp, err := node.isClient.IsDiscovered(publishJobProfile.Key)
			if err == nil && resp.IsDiscovered {
				done <- true
				return
			}

			select {
			case <-time.After(checkInterval):
				continue
			case <-ctx.Done():
				return
			}
		}
	}(ctx)

	select {
	case <-done:
		publishJobProfile.Status = SUCCESS
	case <-time.After(timeout):
		publishJobProfile.Status = TIMEOUT
	}
	cancel()
	node.markPublishJobStatus(jobID, publishJobProfile)
	logMsg(node.name, "checkPublishJobStatusWorker", fmt.Sprintf("Ended, JobID: %v, Status: %d", jobID.String(), publishJobProfile.Status))

}

func (node *Node) maintainPathsHealthWorker() {

	logMsg(node.name, "maintainPathsHealthWorker", fmt.Sprintf("Started, sleep interval = %v", node.v.GetDuration("MAINTAIN_PATHS_HEALTH_CHECKING_INTERVAL")))

	var moveUpWg sync.WaitGroup
	var fixPathWg sync.WaitGroup
	for {
		logMsg(node.name, "maintainPathsHealthWorker", "Iteration Starts")
		for _, pathID := range node.blacklistPathIDs() {
			logMsg(node.name, "maintainPathsHealthWorker", fmt.Sprintf("Handle blacklist path: %v", pathID))
			moveUpWg.Add(1)
			go func(pathID uuid.UUID) {
				node.MoveUp(pathID)
				moveUpWg.Done()
			}(pathID)
		}
		moveUpWg.Wait()
		logMsg(node.name, "maintainPathsHealthWorker", "Blacklist Paths Check Done")

		for _, report := range node.invalidPathProfiles() {
			logMsg(node.name, "maintainPathsHealthWorker", fmt.Sprintf("Handle invalid path: %v %v", report.SelfProfile.uuid, report.HandleType))
			fixPathWg.Add(1)
			go func(report InvalidPathProfile) {
				switch report.HandleType {
				case CLEAN:
					report.SelfProfile.cancelFunc(errors.New("invalid path was cleaned"))
				case FIX:
					report.SelfProfile.next2 = report.NextHopProfile.NextHopIP
					report.SelfProfile.proxyPublic = report.NextHopProfile.ProxyPublicKey
					node.paths.setValue(report.SelfProfile.uuid, report.SelfProfile)
				}
				fixPathWg.Done()
			}(report)
		}
		fixPathWg.Wait()
		logMsg(node.name, "maintainPathsHealthWorker", "Invalid Paths Check Done")
		logMsg(node.name, "maintainPathsHealthWorker", "Iteration Ends")
		time.Sleep(node.v.GetDuration("MAINTAIN_PATHS_HEALTH_CHECKING_INTERVAL"))
	}
}

func (node *Node) maintainPathQuantityWorker() {

	logMsg(node.name, "checkPublishConditionWorker", fmt.Sprintf("Started, sleep interval = %v", node.v.GetDuration("PUBLISH_CONDITION_CHECKING_INTERVAL")))

	for {

		if node.needMorePaths() {
			node.getMorePaths()
		}

		time.Sleep(node.v.GetDuration("PUBLISH_CONDITION_CHECKING_INTERVAL"))
	}

}

func (node *Node) sendCoverMessageWorker(ctx context.Context, connProfile *TCPConnectionProfile, pathID uuid.UUID) {
	defer node.paths.deleteValue(pathID)
	if connProfile == nil || connProfile.Conn == nil || connProfile.Encoder == nil {
		logMsg(node.name, "sendCoverMessageWorker", fmt.Sprintf("sendCoverMessageWorker on path %v failed to start due to connProfile = nil.", pathID.String()))
		return
	}
	conn := *connProfile.Conn
	logMsg(node.name, "sendCoverMessageWorker", fmt.Sprintf("sendCoverMessageWorker to %s on path %v is started successfully.", conn.RemoteAddr().String(), pathID.String()))
	defer conn.Close()

	encoder := connProfile.Encoder
	interval := node.v.GetDuration("COVER_MESSAGE_SENDING_INTERVAL")

	for {
		doneSuccess := make(chan bool)
		doneErr := make(chan error)
		go func() {
			path, _ := node.paths.getValue(pathID)
			coverMsg, err := NewCoverMessage(path.proxyPublic, path.symKey)
			if err != nil {
				doneErr <- err
				return
			}
			err = encoder.Encode(coverMsg)
			if err != nil {
				doneErr <- err
			} else {
				doneSuccess <- true
			}
			defer close(doneSuccess)
			defer close(doneErr)
		}()

		// check if terminated
		select {
		case err := <-doneErr:
			logError(node.name, "sendCoverMessageWorker", err, fmt.Sprintf("error when sending cover message to %s", conn.RemoteAddr().String()))
			return
		case <-doneSuccess:
			// postpone tcp connection deadline
			(*connProfile.Conn).SetDeadline(time.Now().Add(interval).Add(time.Minute))
			logMsg(node.name, "sendCoverMessageWorker", fmt.Sprintf("cover message to %s on path %v is sent successfully.", conn.RemoteAddr().String(), pathID.String()))
		case <-ctx.Done():
			logMsg(node.name, "sendCoverMessageWorker", fmt.Sprintf("TERMINATED BY OTHERS %s", conn.RemoteAddr().String()))
			return
		}
		time.Sleep(interval)
	}
}

func (node *Node) handleApplicationMessageWorker(ctx context.Context, conn net.Conn, coverIp string) {
	defer node.covers.deleteValue(coverIp)
	if conn == nil {
		logMsg(node.name, "handleApplicationMessageWorker", fmt.Sprintf("handleApplicationMessageWorker failed to start due to conn = nil, CoverIP = %v.", coverIp))
		return
	}
	defer conn.Close()
	logMsg(node.name, "handleApplicationMessageWorker", fmt.Sprintf("handleApplicationMessageWorker from %s is started", conn.RemoteAddr().String()))

	decoder := gob.NewDecoder(conn)

	for {
		doneSuccess := make(chan ApplicationMessage)
		doneErr := make(chan error)
		go func() {
			msg := ApplicationMessage{}
			err := decoder.Decode(&msg)
			if err != nil {
				doneErr <- err
			} else {
				doneSuccess <- msg
			}

			defer close(doneSuccess)
			defer close(doneErr)
		}()

		select {
		case msg := <-doneSuccess:
			conn.SetDeadline(time.Now().Add(node.v.GetDuration("APPLICATION_MESSAGE_RECEIVING_INTERVAL")).Add(time.Minute))

			coverProfile, found := node.covers.getValue(coverIp)
			if !found {
				logMsg(node.name, "handleApplicationMessageWorker", fmt.Sprintf("[Cover Not Found]:failed to handle application message from %v", coverIp))
				return
			}
			forwardPathProfile, found := node.paths.getValue(coverProfile.treeUUID)
			if !found {
				logMsg(node.name, "handleApplicationMessageWorker", fmt.Sprintf("[Path Not Found]:failed to handle application message from %v to path %v", coverIp, coverProfile.treeUUID))
				return
			}

			// in the whole handleApplicationMessage scope, should assume coverProfile and forwardPathProfile are valid
			if err := node.handleApplicationMessage(msg, coverProfile, forwardPathProfile); err != nil {
				logError(node.name, "handleApplicationMessageWorker", err, fmt.Sprintf("failed to handle application message from %v to path %v: %v", coverIp, coverProfile.treeUUID, msg))
			}
		case err := <-doneErr:
			logError(node.name, "handleApplicationMessageWorker", err, fmt.Sprintf("error when receiving application message from %s", conn.RemoteAddr().String()))
			return
		case <-time.After(node.v.GetDuration("APPLICATION_MESSAGE_RECEIVING_INTERVAL")):
			logMsg(node.name, "handleApplicationMessageWorker", fmt.Sprintf("handleApplicationMessageWorker from %s: COVER MESSAGE TIMEOUT.\n", conn.RemoteAddr().String()))
			return
		case <-ctx.Done():
			logMsg(node.name, "handleApplicationMessageWorker", fmt.Sprintf("handleApplicationMessageWorker from %s: TERMINATED BY OTHERS.\n", conn.RemoteAddr().String()))
			return
		}
	}
}
