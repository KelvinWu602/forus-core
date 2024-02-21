# Structure

## Major files

### node.go
It implements the forus core. Main.go will instantiate an instance of node. In all documentation, 'node' and 'core' are used interchangably.

### tcp_transport.go
This handles the tcp connection between nodes. The handlers are implemented in node.go. 

### http_transport.go
This handles http communication for the web client. The handlers are implemented in node.go.

### grpc_transport.go
This handles the communication with ND and IS. The handlers are implemented in node.go.

## Helper files

### profiles.go 
This defines PathProfile and CoverNodeProfile.

### messages.go
This defines control messages format.

### keys.go
This defines public key, private key, symmetric keys and key exchange info generation.

### encoding.go
Originally used to encode control messages at tcp. Now deprecated.
