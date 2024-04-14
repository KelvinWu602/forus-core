package p2p

import (
	"log"
	"time"

	"github.com/spf13/viper"
)

// relative to project root
func initConfigs(configFilePath string) *viper.Viper {
	// Environment variables > config.yaml > default values
	v := viper.New()
	// defaults
	// time
	v.SetDefault("COVER_MESSAGE_SENDING_INTERVAL", 10*time.Second)
	v.SetDefault("APPLICATION_MESSAGE_RECEIVING_INTERVAL", 15*time.Second)
	v.SetDefault("PUBLISH_JOB_FAILED_TIMEOUT", 10*time.Minute)
	v.SetDefault("PUBLISH_JOB_CHECKING_INTERVAL", 30*time.Second)
	v.SetDefault("TCP_REQUEST_TIMEOUT", 10*time.Second)
	v.SetDefault("MAINTAIN_PATHS_HEALTH_CHECKING_INTERVAL", 1*time.Minute)
	v.SetDefault("PUBLISH_CONDITION_CHECKING_INTERVAL", 1*time.Minute)
	v.SetDefault("FULFILL_PUBLISH_CONDITION_TIMEOUT", 5*time.Minute)
	v.SetDefault("FULFILL_PUBLISH_CONDITION_INTERVAL", 10*time.Second)
	// int
	v.SetDefault("HALF_OPEN_PATH_BUFFER_SIZE", 5000)
	v.SetDefault("TARGET_NUMBER_OF_CONNECTED_PATHS", 1)
	v.SetDefault("MAXIMUM_NUMBER_OF_COVER_NODES", 15)
	v.SetDefault("NUMBER_OF_COVER_NODES_FOR_PUBLISH", 2)
	v.SetDefault("MOVE_UP_REQUIREMENT_FAILURE_THRESHOLD", 3)
	// string
	v.SetDefault("HTTP_SERVER_LISTEN_PORT", ":3000")

	v.SetDefault("IMMUTABLE_STORAGE_SERVER_LISTEN_IP", "127.0.0.1")
	v.SetDefault("IMMUTABLE_STORAGE_SERVER_LISTEN_PORT", ":3100")

	v.SetDefault("NODE_DISCOVERY_SERVER_LISTEN_IP", "127.0.0.1")
	v.SetDefault("NODE_DISCOVERY_SERVER_LISTEN_PORT", ":3200")
	//bool
	v.SetDefault("HTTP_SERVER_LISTEN_All", "false")
	// config.yaml
	// located at project root folder, same level as main.go
	v.SetConfigFile(configFilePath)
	err := v.ReadInConfig()
	if err != nil {
		log.Println("Error when initConfigs", err, "failed to load config.yaml, using default configs")
	}

	// environment variable override
	// time
	v.BindEnv("COVER_MESSAGE_SENDING_INTERVAL")
	v.BindEnv("APPLICATION_MESSAGE_RECEIVING_INTERVAL")
	v.BindEnv("PUBLISH_JOB_FAILED_TIMEOUT")
	v.BindEnv("PUBLISH_JOB_CHECKING_INTERVAL")
	v.BindEnv("TCP_REQUEST_TIMEOUT")
	v.BindEnv("MAINTAIN_PATHS_HEALTH_CHECKING_INTERVAL")
	v.BindEnv("PUBLISH_CONDITION_CHECKING_INTERVAL")
	v.BindEnv("FULFILL_PUBLISH_CONDITION_TIMEOUT")
	v.BindEnv("FULFILL_PUBLISH_CONDITION_INTERVAL")
	// int
	v.BindEnv("HALF_OPEN_PATH_BUFFER_SIZE")
	v.BindEnv("TARGET_NUMBER_OF_CONNECTED_PATHS")
	v.BindEnv("MAXIMUM_NUMBER_OF_COVER_NODES")
	v.BindEnv("NUMBER_OF_COVER_NODES_FOR_PUBLISH")
	v.BindEnv("MOVE_UP_REQUIREMENT_FAILURE_THRESHOLD")
	// string
	v.BindEnv("HTTP_SERVER_LISTEN_PORT")

	v.BindEnv("IMMUTABLE_STORAGE_SERVER_LISTEN_IP")
	v.BindEnv("IMMUTABLE_STORAGE_SERVER_LISTEN_PORT")

	v.BindEnv("NODE_DISCOVERY_SERVER_LISTEN_IP")
	v.BindEnv("NODE_DISCOVERY_SERVER_LISTEN_PORT")
	// bool
	v.BindEnv("HTTP_SERVER_LISTEN_ALL")
	return v
}
