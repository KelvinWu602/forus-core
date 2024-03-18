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
}

// helper functions
// ==========================================

func (node *Node) markPublishJobStatus(jobID uuid.UUID, profile PublishJobProfile) {
	node.publishJobsRWLock.Lock()
	defer node.publishJobsRWLock.Unlock()
	node.publishJobs[jobID] = profile
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

// cleanDeadCoversWorker will remove cover nodes that have stopped sending cover messages from the local cover node profiles.
func (node *Node) cleanDeadCoversWorker(cleanUpTimeout time.Duration, cleanUpInterval time.Duration) {
	// forever running
	for {
		now := time.Now()
		// for each cover profile, check if current time has passed the lastCoverMessageTime by certain threshold
		node.coversRWLock.Lock()
		for coverIP, coverProfile := range node.covers {
			if now.After(coverProfile.lastCoverMessageTimestamp.Add(cleanUpTimeout)) {
				// the cover node is dead, clean it up
				delete(node.covers, coverIP)
			}
		}
		node.coversRWLock.Unlock()
		time.Sleep(cleanUpInterval)
	}
}

func (node *Node) sendCoverMessageWorker(ctx context.Context, conn net.Conn, interval time.Duration) {
	log.Printf("sendCoverMessageWorker to %s is started successfully.\n", conn.RemoteAddr().String())
	// TODO: create proper cover message
	coverMsg := ApplicationMessage{
		key:     123,
		content: []byte{},
	}

	for {
		// send the cover message
		err := gob.NewEncoder(conn).Encode(coverMsg)
		if err != nil {
			log.Printf("[Error]:sendCoverMessageWorker when sending cover message to %s: %v\n", conn.RemoteAddr().String(), err)
		}
		time.Sleep(interval)

		// check if terminated
		if len(ctx.Done()) > 0 {
			<-ctx.Done()
			log.Printf("sendCoverMessageWorker to %s is stopped successfully.\n", conn.RemoteAddr().String())
			return
		}
	}
}
