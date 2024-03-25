package p2p

import "time"

const COVER_MESSAGE_SENDING_INTERVAL time.Duration = 10 * time.Second
const APPLICATION_MESSAGE_RECEIVING_INTERVAL time.Duration = 15 * time.Second
const HALF_OPEN_PATH_BUFFER_SIZE int = 10000
const TCP_REQUEST_TIMEOUT time.Duration = 10 * time.Second
const TARGET_NUMBER_OF_CONNECTED_PATHS int = 3
const MAXIMUM_NUMBER_OF_COVER_NODES int = 15
const TCP_SERVER_LISTEN_PORT string = ":3001"
const HTTP_SERVER_LISTEN_PORT string = ":3000"
