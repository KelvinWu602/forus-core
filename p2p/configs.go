package p2p

import "time"

const COVER_MESSAGE_SENDING_INTERVAL time.Duration = 10 * time.Second
const APPLICATION_MESSAGE_RECEIVING_INTERVAL time.Duration = 15 * time.Second
const PUBLISH_JOB_FAILED_TIMEOUT time.Duration = 10 * time.Minute
const PUBLISH_JOB_CHECKING_INTERVAL time.Duration = 30 * time.Second
const TCP_REQUEST_TIMEOUT time.Duration = 10 * time.Second
const MOVE_UP_REQUIREMENT_CHECKING_INTERVAL time.Duration = 1 * time.Minute

const HALF_OPEN_PATH_BUFFER_SIZE int = 10000
const TARGET_NUMBER_OF_CONNECTED_PATHS int = 3
const MAXIMUM_NUMBER_OF_COVER_NODES int = 15
const NUMBER_OF_COVER_NODES_FOR_PUBLISH int = 2
const MOVE_UP_REQUIREMENT_FAILURE_THRESHOLD int = 3

const TCP_SERVER_LISTEN_PORT string = ":3001"
const HTTP_SERVER_LISTEN_PORT string = ":3000"
const NODE_DISCOVERY_SERVER_LISTEN_PORT string = ":3200"
const IMMUTABLE_STORAGE_SERVER_LISTEN_PORT string = ":3100"
