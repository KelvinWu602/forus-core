# Forus-Core

## How to run as Docker Container
```bash

# create docker image
docker build -t forus-core .

# start docker container
docker run forus-core
```

## How to run test locally

Execute the go test files one by one in the following order:
1. publish_forward_test.go
2. cover_node_test.go
3. maintain_path_health_test.go
4. maintain_path_valid_test.go

## How to run test remotely on EC2
