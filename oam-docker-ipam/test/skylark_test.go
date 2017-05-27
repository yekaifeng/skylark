package gotest

import (
	"testing"
	"fmt"
)

func Test_FailAllocateIP(t *testing.T) {
	init_env()
	t.Log("Test FailAllocateIP Start ...")
}

func Test_FailReleaseIP(t *testing.T) {
	init_env()
	t.Log("Test FailReleaseIP Start ...")
}

func init_env() {
	fmt.Println("init the environment ...")

}