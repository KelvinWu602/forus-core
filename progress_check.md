# main.go

## func main() 

- replace empty select statement with a SIGINT catcher (@KelvinWu602)
    - edit MakeServerAndStart as well


# node.go

## func initGobTypeRegistration()

- double check if all required types are registered (@KelvinWu602)

## func MakeServerAndStart()

- make StartHTTP() and StartTCP() consistent, move addr to configs.go (@KelvinWu602) - Done

## func formTree()

- simplify formTree() design, currently it is too complicated. (@KelvinWu602)
    - create a cancellable worker `populateHalfOpenPaths()`.
    - formTree will start to consume halfOpenPaths.
    - if it is empty, start the worker.
    - notify the worker to stop when k paths are formed.

## func handleConnectPathReq()

- add checking on number of cover nodes (@KelvinWu602) - Done
    - use configs.go

## func handleCreateProxyReq()

- add checking on number of cover nodes (@KelvinWu602) - Done
    - use configs.go

## func handleDeleteCoverReq()

- delete unnecessary guard around delete() (@KelvinWU602) - Done

## func handleRealMessage()

- verify behavior of asymmetricDecrypt, see if the module using a wrong private key to decrypt will report error (@SauDoge6597) DONE
  - Ecc will return a Invalid MAC Error if the wrong private key is used (refer to codec_test.go TestAsymmetricDecryptInWrongKey for an example)
  - Rewrite AsymmetricDecrypt() to returning an "wrong private key" error as with an empty []byte as the decryption
- store in ImmutableStorage (@KelvinWu602) - Done
- call Forward (@KelvinWu602) - Done

## func ConnectPath()

- need a way to externalize TCP connection for later Publish (@KelvinWu602)
    - `ConnectPath()`: Should store the TCP connection in member map `OpenConnections map[uuid.UUID]*net.Conn`
    - `Publish()`: 
        - randomly select a connection in member map, send until no error. if error then use another path
            - suppose `sendCoverMessageWorker()` is healthchecking each connection, so no need to clean up failed connection. 
        - Initiate `checkPublishJobStatusWorker()` at the end of `Publish()`

## func MoveUp()

- finish it... not yet think how to do (@SauDoge6597)
- Draft:
    - member `pathAnalytics`
        - MovingUp
        - SuccessCount
        - FailureCount
        - other related things, e.g. average job complete time
    - `checkPublishJobStatusWorker()` will update `pathAnalytics`
    - `func checkMoveUpRequirementsWorker()`: 
        - periodically base on pathAnalytics, decide if a path is malicious
        - if yes, call MoveUp(pathID uuid.UUID) in separate goroutine (non-blocking)
            - MoveUp should backup the path info (for next next hop, proxy key, uuid), then delete the path 
            - Use the path info, decide；
                - If next next hop is a node, connect to it
                    - i.e. formTree() with the next-next hop
                - If failed or next next hop is IPFS, become a proxy
                    - Create a new path profile
                    - next hop = IPFS
                    - next next hop = ""
                    - proxyPublicKey = self public key
                    - pathID = new uuid

# grpc_transport.go

## func initNodeDiscoverClient()

- No need to cache the membners result, as it is expected to be frequently changing (@SauDoge6597) DONE





