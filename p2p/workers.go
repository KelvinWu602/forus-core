package p2p

import (
	"context"
	"encoding/gob"
	"errors"
	"fmt"
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

	doneSuccess := make(chan bool)
	doneErr := make(chan error)
	defer close(doneSuccess)
	defer close(doneErr)

	for {
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
		}()

		// check if terminated
		select {
		case err := <-doneErr:
			logError(node.name, "sendCoverMessageWorker", err, fmt.Sprintf("error when sending cover message to %s", conn.RemoteAddr().String()))
			return
		case <-doneSuccess:
			// postpone tcp connection deadline
			(*connProfile.Conn).SetDeadline(time.Now().Add(node.v.GetDuration("COVER_MESSAGE_SENDING_INTERVAL")).Add(time.Minute))
			logMsg(node.name, "sendCoverMessageWorker", fmt.Sprintf("cover message to %s on path %v is sent successfully.", conn.RemoteAddr().String(), pathID.String()))
			time.Sleep(node.v.GetDuration("COVER_MESSAGE_SENDING_INTERVAL"))
		case <-ctx.Done():
			logMsg(node.name, "sendCoverMessageWorker", fmt.Sprintf("TERMINATED BY OTHERS %s", conn.RemoteAddr().String()))
		}
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

	doneSuccess := make(chan ApplicationMessage)
	doneErr := make(chan error)

	for {
		go func() {
			msg := ApplicationMessage{}
			err := decoder.Decode(&msg)
			if err != nil {
				doneErr <- err
			} else {
				doneSuccess <- msg
			}
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
