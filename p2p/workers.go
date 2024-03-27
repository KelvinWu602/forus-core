package p2p

import (
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
}

// helper functions
// ==========================================

func (node *Node) markPublishJobStatus(jobID uuid.UUID, profile PublishJobProfile) {
	node.publishJobsRWLock.Lock()
	defer node.publishJobsRWLock.Unlock()
	node.publishJobs[jobID] = profile
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
func (node *Node) checkPublishJobStatusWorker(jobID uuid.UUID, timeout time.Duration, checkInterval time.Duration) {
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

func (n *Node) checkMoveUpReqWorker(checkInterval time.Duration) {

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
