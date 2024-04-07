package p2p

import (
	"context"
	"encoding/gob"
	"fmt"
	"net"
	"sync"
	"time"

	"golang.org/x/exp/slices"

	"github.com/google/uuid"
	"github.com/spf13/viper"
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
		if path.failureCount >= viper.GetInt("MOVE_UP_REQUIREMENT_FAILURE_THRESHOLD") {
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
		nextHopAddr := oldPath.next
		resp, err := node.sendQueryPathRequest(nextHopAddr)
		if err != nil {
			// next-hop is unreachable, should clean this path
			results = append(results, InvalidPathProfile{SelfProfile: oldPath, HandleType: CLEAN})
			return
		}

		for _, newPath := range resp.Paths {
			newPathID, err := DecryptUUID(newPath.EncryptedTreeUUID, node.privateKey)
			if err == nil && newPathID == pathID && (oldPath.next2 != newPath.NextHop || !slices.Equal(oldPath.proxyPublic, newPath.ProxyPublicKey)) {
				// inconsistent path data, should fix local path
				results = append(results, InvalidPathProfile{SelfProfile: oldPath, NextHopProfile: newPath, HandleType: FIX})
				return
			}
		}

		// cannot find this path on next-hop, should clean this path
		results = append(results, InvalidPathProfile{SelfProfile: oldPath, HandleType: CLEAN})
	}, true)

	return results
}

func (node *Node) canPublish(lock bool) bool {
	if lock {
		return node.covers.getSize() >= viper.GetInt("NUMBER_OF_COVER_NODES_FOR_PUBLISH") &&
			node.paths.getSize() >= viper.GetInt("TARGET_NUMBER_OF_CONNECTED_PATHS")
	}
	return len(node.covers.data) >= viper.GetInt("NUMBER_OF_COVER_NODES_FOR_PUBLISH") &&
		len(node.paths.data) >= viper.GetInt("TARGET_NUMBER_OF_CONNECTED_PATHS")
}

// workers
// ==========================================

// publishJobStatusChecker will monitor the status of a publish job for a given timeout.
func (node *Node) checkPublishJobStatusWorker(jobID uuid.UUID) {

	logMsg("checkPublishJobStatusWorker", fmt.Sprintf("Started, JobID: %v", jobID.String()))

	timeout := viper.GetDuration("PUBLISH_JOB_FAILED_TIMEOUT")
	checkInterval := viper.GetDuration("PUBLISH_JOB_CHECKING_INTERVAL")

	// check if the job really exists
	publishJobProfile, jobExists := node.publishJobs.getValue(jobID)
	if !jobExists {
		logMsg("checkPublishJobStatusWorker", fmt.Sprintf("JobID %v does not exist", jobID.String()))
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
	logMsg("checkPublishJobStatusWorker", fmt.Sprintf("Ended, JobID: %v, Status: %d", jobID.String(), publishJobProfile.Status))

}

func (node *Node) maintainPathsHealthWorker() {

	logMsg("maintainPathsHealthWorker", "Started")

	var moveUpWg sync.WaitGroup
	var fixPathWg sync.WaitGroup
	for {
		logMsg("maintainPathsHealthWorker", "Iteration Starts")
		for _, pathID := range node.blacklistPathIDs() {
			moveUpWg.Add(1)
			go func(pathID uuid.UUID) {
				node.MoveUp(pathID)
				moveUpWg.Done()
			}(pathID)
		}
		moveUpWg.Wait()
		logMsg("maintainPathsHealthWorker", "Blacklist Paths Check Done")

		for _, report := range node.invalidPathProfiles() {
			fixPathWg.Add(1)
			go func(report InvalidPathProfile) {
				switch report.HandleType {
				case CLEAN:
					node.paths.deleteValue(report.SelfProfile.uuid)
				case FIX:
					report.SelfProfile.next2 = report.NextHopProfile.NextHop
					report.SelfProfile.proxyPublic = report.NextHopProfile.ProxyPublicKey
					node.paths.setValue(report.SelfProfile.uuid, report.SelfProfile)
				}
				fixPathWg.Done()
			}(report)
		}
		fixPathWg.Wait()
		logMsg("maintainPathsHealthWorker", "Invalid Paths Check Done")
		logMsg("maintainPathsHealthWorker", "Iteration Ends")
		time.Sleep(viper.GetDuration("MAINTAIN_PATHS_HEALTH_CHECKING_INTERVAL"))
	}
}

func (node *Node) checkPublishConditionWorker() {

	logMsg("checkPublishConditionWorker", "Started")

	for {

		if !node.canPublish(true) {
			node.fulfillPublishCondition()
		}

		time.Sleep(viper.GetDuration("PUBLISH_CONDITION_CHECKING_INTERVAL"))
	}

}

func (node *Node) sendCoverMessageWorker(conn net.Conn, pathID uuid.UUID) {

	logMsg("sendCoverMessageWorker", fmt.Sprintf("sendCoverMessageWorker to %s on path %v is started successfully.", conn.RemoteAddr().String(), pathID.String()))

	encoder := gob.NewEncoder(conn)

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
			logError("sendCoverMessageWorker", err, fmt.Sprintf("error when sending cover message to %s", conn.RemoteAddr().String()))
			node.paths.deleteValue(pathID)
			return
		case <-doneSuccess:

			logMsg("sendCoverMessageWorker", fmt.Sprintf("cover message to %s on path %v is sent successfully.", conn.RemoteAddr().String(), pathID.String()))

			time.Sleep(viper.GetDuration("COVER_MESSAGE_SENDING_INTERVAL"))
		}
	}
}

func (node *Node) handleApplicationMessageWorker(conn net.Conn) {
	logMsg("handleApplicationMessageWorker", fmt.Sprintf("handleApplicationMessageWorker from %s is started", conn.RemoteAddr().String()))

	coverIp := conn.RemoteAddr().String()
	decoder := gob.NewDecoder(conn)

	doneSuccess := make(chan ApplicationMessage)
	doneErr := make(chan error)
	defer close(doneSuccess)
	defer close(doneErr)

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
			if err := node.handleApplicationMessage(msg, coverIp); err != nil {
				logError("handleApplicationMessageWorker", err, fmt.Sprintf("failed to handle application message from %v: %v", coverIp, msg))
			}
		case err := <-doneErr:
			logError("handleApplicationMessageWorker", err, fmt.Sprintf("error when receiving application message from %s", conn.RemoteAddr().String()))
			node.covers.deleteValue(coverIp)
			return
		case <-time.After(viper.GetDuration("APPLICATION_MESSAGE_RECEIVING_INTERVAL")):
			logMsg("handleApplicationMessageWorker", fmt.Sprintf("handleApplicationMessageWorker from %s: COVER MESSAGE TIMEOUT.\n", conn.RemoteAddr().String()))
			node.covers.deleteValue(coverIp)
			return
		}
	}
}
