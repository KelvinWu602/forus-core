FROM golang:1.20

WORKDIR /forus-core
COPY go.mod go.sum ./
RUN go mod download && go mod verify

COPY . .
RUN go build -o /usr/local/bin/forus-core

CMD ["forus-core"]