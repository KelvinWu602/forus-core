package p2p

import (
	"os"
	"testing"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
)

var HI = "123"
var num = 234
var haha = 1

func TestViper(t *testing.T) {
	// Env var > config > default

	viper.SetDefault("HI", "123")
	viper.SetDefault("num", 123)
	viper.SetDefault("haha", 1)

	viper.SetEnvPrefix("FORUS")
	viper.BindEnv("HI")

	viper.SetConfigFile("../config.yaml")
	viper.SetConfigName("config")
	viper.SetConfigType("yaml")
	viper.AddConfigPath("..")
	err := viper.ReadInConfig()
	if err != nil {
		t.Fatal(err)
	}
	os.Setenv("FORUS_HI", "environment")

	HI = viper.GetString("HI")

	num = viper.GetInt("num")

	haha = viper.GetInt("haha")

	v, f := os.LookupEnv("FORUS_HI")

	// check validness
	assert := assert.New(t)
	assert.Equal(true, f, "env found should be true")
	assert.Equal("environment", v, "env should be environment")
	assert.Equal("environment", HI, "should be environment")
	assert.Equal(123, num, "should be 234")
	assert.Equal(1, haha, "should be 1")
}
