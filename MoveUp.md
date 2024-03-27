# Move Up

## Move Up Condition
There are two possible scenarios where moveUp should be called:

### Scenario 1: Actively move up due to failed Jobs

1. For each path, the node will maintain a `pathAnalytics` as a profile.
    - successCount
    - failureCount
2. `checkPublishJobStatusWorker()` updates `pathAnalytics`:
    - if job is successful: successCount += 1
    - if job fails to publish: failureCount += 1
3. `checkMoveUpRequirementsWorker()` periodically checks `pathAnalytics`:
    - if failureCount >= X: `go MoveUpVoluntarily(oldUUID)`
### Scenario 2: Passively move up due to next hop moving up

1. every node's `checkMoveUpRequirementsWorker()` periodically calls `QueryPath` with next-hop
2. If there exists an path with the same treeUUID but self.next_next != next.next -> the next-hop has moved up
    - `go MoveUpInvoluntarily(oldUUID, false)`
3. If there exists a new path where next is IPFS -> the next-hop has become a proxy
    - `go MoveUpInvoluntarily(oldUUID, true)`

## MoveUpVoluntarily() Implementation
1. Given the treeUUID, find the original_next and original_next_next in self.paths
2. sendVerifyCover(original_next) to original_next_next
3. If verifyCover succeed: move to 6
4. If verifyCover faileed: move to 5
5. If original_next_next == "IPFS" -> self will become proxy
    - create a newPathProfile = {next: IPFS, next_next: null, proxyPub: self.pub, pathID: newUUID}
    - remove the oldPathProfile = {pathID: oldUUID}
    - all the cover node in self.covers that belongs to oldUUID needs to be removed as well 
6. If original_next_next == node -> self should try to connectPath(original_next_next)
   - If failed, self needs to become proxy and repeat (2)

## MoveUpInvoluntarily(oldUUID, isNextProxy) Implementation
1. Given the treeUUID, find the original_next and original_next_next in self.paths
2. If isNextProxy == false:
    - simply modify self.paths from {next: original_next, next_next: original_next_next}, {next: original_next, next_next: new_next_next}
4. If isNextProxy == true: need to establish new tree
    - remove oldPath {pathID: oldUUID} from self.paths
    - connectPath(original_next)
