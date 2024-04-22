# TODOs

DEADLINE: 20 March 2024 Tentative

## CORE HTTP (TODO: @SauDoge)
### NEW(): main function (TODO: update implementation)
1. Ensure tree formation is completed before HTTP server boot

2. Remove JoinCluster() and LeaveCluster() in HTTP endpoints
- JoinCluster()'s functionality should be incorporate into formTree()
- LeaveCluster() should be triggered by CTRL+C
(refer to IS)

### GET /message queryString: key (TODO: update implementation)
- call IS.read()

Possible return is IS.Read()
1. Normal: message body
Handle: list of message body -> json
2. If the key does not exist in IS:
Handle: omit the entry with error in the json

### POST /message (TODO: update implementation)
- Content-Type/json {key: message}
- call Publish(key Key, message []byte) // check Immutable Storage
- if validate key error: return 400 BadRequest
- if anonymity condition error: return 500 BadServer

### GET /checkPublishingStatus queryString: publishingID (TODO: create new implementation)
- call IS.read()
- Handle: return status of publishing JobId

### Publish() (TODO: update implementation)
1. Validate Key via IS.ValidateKey(key, message)
  If failed, return validate key error
2. Check tree formation status
  If failed, return anonymity error
3. Pick a random path and get the conn
4. Asymmetric Encryption
5. Symmetric Encryption
6. encode and send it through via gob and conn
A new control message type: "data"
7. update map<publishingJobID, Profile{key, status}>
8. return entry<publishingJobID, Profile{key, status}>


// Forward() is only called when the node realised it is a real message and the node is not the proxy
// The identification is done in handleRealMessage()
### Forward(message []byte) (TODO: update implementation)
1. symmetric encryption and send to next hop on the same path (recognised from the profile)

--------------------------------------------
## Encryption Scheme (TODO: @SauDoge)
- encoding and decoding of ecc

----------------------------------------------
## NUMBER 1 PRIORITY: Tree Formation (TODO: @KelvinWu602)

### UNDER no.1: TCP Connection (TODO: @KelvinWu602)
- Make sure the ports are correct
- Close all orphan connection
  Principle: QueryPath, ConnectPath should close all conns after completion of function

### UNDER no.1: Handler structure problem (TODO: @KelvinWu602)
Problem: QueryPath() won't know when the verify cover is success, therefore fail to couple 
Intermediate solution 1: use a channel to listen to VerifyCover, trigger the next step when sth is returned
Intermediate problem 1: if two verify cover resp enters (which a person can manually create such TCP message), fucked up the channel with false resp
Intermediate solution 2: TODO

# Failure Detection (TODO: @KelvinWu602)
1. Detect if the path has malicious behaviour
- Maybe a worker looking at the publishingJobIDs
2. Monitor periodically if the cover node is dead
- If someone connectPath to you, you need a worker to check the frequency of cover

## Cover messages (TODO: @KelvinWu602)
1. Need a go routine to send cover message periodically after connectPath

### Minor (TODO: @SauDoge)
1. CORS header in HTTP response 

## IS

## Frontend 
### 


testing: error half open path not found is because
when query path, send verify cover
next next hop's cover profile for the middle node use key : 127.0.0.1:xxxxx 
in essence on localhost, the only ID of a node is the server listening port
but the cover node can only be identified by sending port != listening port anyway, will never be verified
unless can fix the sending port, making it must be listening port +1

then change verify condition logic to use breakhostport(content.nextHop).port+1 to reconstruct xxxxx



=====
path delete should trigger stop of send cover msg worker

paths creation:
- connect path
- create proxy
- handle create proxy
- move up
- post path handler

paths modification:
- mark publish job done
- fix invalid path

paths delete
- moveup
- clean invalid path
- send cover msg
===================

paths creation:
- connect path
- create proxy
- handle create proxy
- move up
- post path handler

paths modification:
- mark publish job done
- fix invalid path

paths delete
- terminate send cover msg worker

pass maintain_path_health_test