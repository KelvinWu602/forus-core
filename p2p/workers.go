package p2p

import (
	"context"
	"encoding/gob"
	"log"
	"net"
	"time"

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

// helper functions
// ==========================================

func (node *Node) markPublishJobStatus(jobID uuid.UUID, profile PublishJobProfile) {
	node.publishJobsRWLock.Lock()
	node.paths[profile.OnPath].pathStat.CountLock.Lock()
	defer node.publishJobsRWLock.Unlock()
	defer node.paths[profile.OnPath].pathStat.CountLock.Unlock()
	node.publishJobs[jobID] = profile
	if profile.Status == SUCCESS {
		node.paths[profile.OnPath].pathStat.WriteSuccessCount(1)
	} else {
		node.paths[profile.OnPath].pathStat.WriteFailureCount(1)
	}
}

func (node *Node) removeCoversProfile(coverIp string) {
	node.coversRWLock.Lock()
	defer node.coversRWLock.Unlock()
	delete(node.covers, coverIp)
}

func (node *Node) removePathProfile(pathID uuid.UUID) {
	node.pathsRWLock.Lock()
	defer node.pathsRWLock.Unlock()
	delete(node.paths, pathID)
}

// workers
// ==========================================

// publishJobStatusChecker will monitor the status of a publish job for a given timeout.
func (node *Node) checkPublishJobStatusWorker(jobID uuid.UUID) {
	timeout := PUBLISH_JOB_FAILED_TIMEOUT
	checkInterval := PUBLISH_JOB_CHECKING_INTERVAL

	// check if the job really exists
	node.publishJobsRWLock.RLock()
	publishJobProfile, jobExists := node.publishJobs[jobID]
	node.publishJobsRWLock.RUnlock()
	if !jobExists {
		return
	}

	// channel for the inner function to report success
	done := make(chan bool)
	// channel for the checker to inform the inner function to stop, required as child goroutine will not stop even if parent goroutine stopped.
	terminateSignal := make(chan bool)
	defer close(done)
	defer close(terminateSignal)
	go func() {
		for {
			if len(terminateSignal) > 0 {
				<-terminateSignal
				return
			}
			resp, err := node.isClient.IsDiscovered(publishJobProfile.Key)
			if err == nil && resp.IsDiscovered {
				done <- true
				return
			}
			time.Sleep(checkInterval)
		}
	}()

	select {
	case <-done:
		publishJobProfile.Status = SUCCESS
	case <-time.After(timeout):
		terminateSignal <- true
		publishJobProfile.Status = TIMEOUT
	}
	node.markPublishJobStatus(jobID, publishJobProfile)
}

func (n *Node) checkMoveUpReqWorker(failureThreshold int, checkInterval time.Duration) {

	checkDone := make(chan bool)
	defer close(checkDone)

	for {
		go func() {
			// active check
			// for every path, check if FailureCount > failureThreshold
			// if true, move up
			// if false, continue
			n.pathsRWLock.Lock()
			for k, v := range n.paths {
				v.pathStat.CountLock.Lock()
				failureCount := v.pathStat.ReadFailureCount()
				if failureCount < failureThreshold {
					continue
				} else {
					go n.MoveUpVoluntarily(k)
				}
			}
			n.pathsRWLock.Unlock()
			// passive check
			// query every next hop and get their paths if the following exists:
			// a) same tree uuid but self.next_next != resp.next
			// b) tree uuid that is not in n.paths but resp.next == proxy
			n.pathsRWLock.Lock()
			for oldTreeUUID, oldPath := range n.paths {
				resp, err := n.sendQueryPathRequest(oldPath.next)
				if err != nil {
					continue // remove path from path profile
				}
				for _, newPath := range resp.Paths {
					decryptedTreeUUID, err := DecryptUUID(newPath.EncryptedTreeUUID, n.privateKey)
					if err != nil {
						continue
					}

					path, ok := n.paths[decryptedTreeUUID]

					if ok { // check (a)
						if path.next2 != newPath.NextHop {
							// the move up will need to modify oldTreeID by replacing originalNext2 with newNext2
							go n.MoveUpInvoluntarily(oldTreeUUID, false, newPath.NextHop)
						}
					} else { // check (b)
						if newPath.NextHop == "ImmutableStorage" {
							// the move up will need to remove oldPath and connectPath with newTreeID
							go n.MoveUpInvoluntarily(decryptedTreeUUID, true, oldPath.next)
						}

					}
				}
			}
			n.pathsRWLock.Unlock()
			checkDone <- true
		}()

		select {
		case <-checkDone:
			time.Sleep(checkInterval)
		case <-n.ctrlCSignalPropagator.Done():
			return
		}
	}

}

func (node *Node) sendCoverMessageWorker(conn net.Conn, interval time.Duration, pathID uuid.UUID) {
	log.Printf("sendCoverMessageWorker to %s is started successfully.\n", conn.RemoteAddr().String())

	encoder := gob.NewEncoder(conn)

	doneSuccess := make(chan bool)
	doneErr := make(chan error)
	defer close(doneSuccess)
	defer close(doneErr)

	for {
		go func() {
			path := node.paths[pathID]
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
			log.Printf("[Error]:sendCoverMessageWorker when sending cover message to %s: %v\n", conn.RemoteAddr().String(), err)
			node.removePathProfile(pathID)
			return
		case <-node.ctrlCSignalPropagator.Done():
			node.removePathProfile(pathID)
			log.Printf("sendCoverMessageWorker to %s is stopped successfully.\n", conn.RemoteAddr().String())
			return
		case <-doneSuccess:
			time.Sleep(interval)
		}
	}
}

func (node *Node) handleApplicationMessageWorker(conn net.Conn, maxInterval time.Duration) {
	log.Printf("handleApplicationMessageWorker from %s is started successfully.\n", conn.RemoteAddr().String())

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
				log.Printf("[Error]:handleApplicationMessageWorker when receiving application message from %s: %v\n", conn.RemoteAddr().String(), err)
				doneErr <- err
			} else {
				doneSuccess <- msg
			}
		}()

		select {
		case msg := <-doneSuccess:
			if err := node.handleApplicationMessage(msg, coverIp); err != nil {
				log.Printf("[Error]:handleApplicationMessage: %v\n", err)
			}
		case err := <-doneErr:
			log.Printf("handleApplicationMessageWorker from %s: RECEIVE ERROR: %v\n", conn.RemoteAddr().String(), err)
			node.removeCoversProfile(coverIp)
			return
		case <-time.After(maxInterval):
			log.Printf("handleApplicationMessageWorker from %s: COVER MESSAGE TIMEOUT.\n", conn.RemoteAddr().String())
			node.removeCoversProfile(coverIp)
			return
		case <-node.ctrlCSignalPropagator.Done():
			log.Printf("handleApplicationMessageWorker from %s: CANCEL SIGNAL RECEIVED.\n", conn.RemoteAddr().String())
			return
		}
	}
}

// Iterate all cluster members infinitely many times, and perform query path on all of them. Cancellable.
func (node *Node) populateHalfOpenPathsWorker(ctx context.Context) {
	for {
		// 1 iteration of looping through all cluster members
		resp, err := node.ndClient.GetMembers()
		if err != nil {
			log.Printf("[populateHalfOpenPathsWorker]:Error:%v\n", err)
			return
		}
		done := make(chan bool)
		for _, memberIP := range resp.Member {
			// looping through all cluster members
			addr := memberIP + TCP_SERVER_LISTEN_PORT
			go func() {
				// call QueryPath, which will append to the halfOpenPaths, will block when pendingHalfOpenPath is full.
				node.QueryPath(addr)
				done <- true
			}()
			select {
			case <-done:
				continue
			case <-ctx.Done():
				return
			}
		}
	}
}
