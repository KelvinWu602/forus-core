package p2p

import (
	"time"

	"github.com/spf13/viper"
)

func initConfigs() {
	// Environment variables > config.yaml > default values

	// defaults
	// time
	viper.SetDefault("COVER_MESSAGE_SENDING_INTERVAL", 10*time.Second)
	viper.SetDefault("APPLICATION_MESSAGE_RECEIVING_INTERVAL", 15*time.Second)
	viper.SetDefault("PUBLISH_JOB_FAILED_TIMEOUT", 10*time.Minute)
	viper.SetDefault("PUBLISH_JOB_CHECKING_INTERVAL", 30*time.Second)
	viper.SetDefault("TCP_REQUEST_TIMEOUT", 10*time.Second)
	viper.SetDefault("MAINTAIN_PATHS_HEALTH_CHECKING_INTERVAL", 1*time.Minute)
	viper.SetDefault("PUBLISH_CONDITION_CHECKING_INTERVAL", 1*time.Minute)
	viper.SetDefault("FULFILL_PUBLISH_CONDITION_TIMEOUT", 5*time.Minute)
	// int
	viper.SetDefault("HALF_OPEN_PATH_BUFFER_SIZE", 10000)
	viper.SetDefault("TARGET_NUMBER_OF_CONNECTED_PATHS", 3)
	viper.SetDefault("MAXIMUM_NUMBER_OF_COVER_NODES", 15)
	viper.SetDefault("NUMBER_OF_COVER_NODES_FOR_PUBLISH", 2)
	viper.SetDefault("MOVE_UP_REQUIREMENT_FAILURE_THRESHOLD", 3)
	// string
	viper.SetDefault("TCP_SERVER_LISTEN_PORT", ":3001")
	viper.SetDefault("HTTP_SERVER_LISTEN_PORT", ":3000")
	viper.SetDefault("NODE_DISCOVERY_SERVER_LISTEN_PORT", ":3200")
	viper.SetDefault("IMMUTABLE_STORAGE_SERVER_LISTEN_PORT", ":3100")
	// bool
	viper.SetDefault("TESTING_FLAG", false)

	// config.yaml
	viper.SetConfigFile("./config.yaml")
	viper.SetConfigName("config")
	viper.SetConfigType("yaml")
	// project root folder, ame level as main.go
	viper.AddConfigPath(".")
	err := viper.ReadInConfig()
	if err != nil {
		logError("initConfigs", err, "failed to load config.yaml, use default configs")
	}

	// environment variable override
	// time
	viper.BindEnv("COVER_MESSAGE_SENDING_INTERVAL")
	viper.BindEnv("APPLICATION_MESSAGE_RECEIVING_INTERVAL")
	viper.BindEnv("PUBLISH_JOB_FAILED_TIMEOUT")
	viper.BindEnv("PUBLISH_JOB_CHECKING_INTERVAL")
	viper.BindEnv("TCP_REQUEST_TIMEOUT")
	viper.BindEnv("MAINTAIN_PATHS_HEALTH_CHECKING_INTERVAL")
	viper.BindEnv("PUBLISH_CONDITION_CHECKING_INTERVAL")
	viper.BindEnv("FULFILL_PUBLISH_CONDITION_TIMEOUT")
	// int
	viper.BindEnv("HALF_OPEN_PATH_BUFFER_SIZE")
	viper.BindEnv("TARGET_NUMBER_OF_CONNECTED_PATHS")
	viper.BindEnv("MAXIMUM_NUMBER_OF_COVER_NODES")
	viper.BindEnv("NUMBER_OF_COVER_NODES_FOR_PUBLISH")
	viper.BindEnv("MOVE_UP_REQUIREMENT_FAILURE_THRESHOLD")
	// string
	viper.BindEnv("TCP_SERVER_LISTEN_PORT")
	viper.BindEnv("HTTP_SERVER_LISTEN_PORT")
	viper.BindEnv("NODE_DISCOVERY_SERVER_LISTEN_PORT")
	viper.BindEnv("IMMUTABLE_STORAGE_SERVER_LISTEN_PORT")
	viper.BindEnv("IMMUTABLE_STORAGE_SERVER_LISTEN_PORT")
	viper.BindEnv("CLUSTER_CONTACT_NODE_IP")
	// bool
	viper.BindEnv("TESTING_FLAG")
}
