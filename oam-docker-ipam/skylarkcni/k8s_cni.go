/***
Copyright 2016 Cisco Systems Inc. All rights reserved.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at
http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"io/ioutil"

	"oam-docker-ipam/skylarkcni/cniapi"
	"oam-docker-ipam/skylarkcni/clients"
	"github.com/containernetworking/cni/pkg/types"

	logger "github.com/Sirupsen/logrus"
)

//CNIError : return format from CNI plugin
type CNIError struct {
	CNIVersion string `json:"cniVersion"`
	Code       uint   `json:"code"`
	Msg        string `json:"msg"`
	Details    string `json:"details,omitempty"`
}

var log *logger.Entry

func getPodInfo(ppInfo *cniapi.CNIPodAttr) error {
	cniArgs := os.Getenv("CNI_ARGS")
	if cniArgs == "" {
		return fmt.Errorf("Error reading CNI_ARGS")
	}

	// convert the cniArgs to json format
	cniArgs = "{\"" + cniArgs + "\"}"
	cniTmp1 := strings.Replace(cniArgs, "=", "\":\"", -1)
	cniJSON := strings.Replace(cniTmp1, ";", "\",\"", -1)
	err := json.Unmarshal([]byte(cniJSON), ppInfo)
	if err != nil {
		return fmt.Errorf("Error parsing cni args: %s", err)
	}

	// nwNameSpace and ifname are passed as separate env vars
	ppInfo.NwNameSpace = os.Getenv("CNI_NETNS")
	ppInfo.IntfName = os.Getenv("CNI_IFNAME")
	return nil
}

func addPodToNet(nc *clients.NWClient, pInfo *cniapi.CNIPodAttr, netconf *types.NetConf) {

	// Add Pod to network
	result, err := nc.RequestAddress(pInfo, netconf)
	if err != nil  {
		log.Errorf("EP create failed for pod: %s/%s",
			pInfo.K8sNameSpace, pInfo.Name)
		os.Exit(1)
	}

        if netconf.Type == "bridge" {
		err := cmdAdd(pInfo, netconf, result.Address)
		if err != nil {
			log.Errorf("fail to add pod to net %v", err)
			fmt.Printf(err)
		}
	}

	if netconf.Type == "macvlan" {

	}

	log.Infof("EP created IP: %s\n", result.Address)
	// Write the ip address of the created endpoint to stdout
	fmt.Printf("{\n\"cniVersion\": \"0.1.0\",\n")
	fmt.Printf("\"ip4\": {\n")
	fmt.Printf("\"ip\": \"%s\"\n}\n}\n", result.Address)
}

func deletePodFromNet(nc *clients.NWClient, pInfo *cniapi.CNIPodAttr, netconf *types.NetConf) {
	//Query ip address by infracontainer id
	ipaddress, err := nc.GetAddress(pInfo.InfraContainerID)
	if err != nil {
		log.Errorf("Failed to get ip address for %s, %v",pInfo.InfraContainerID, err)
	}

	err = nc.ReleaseAddress(netconf, ipaddress)
	if err != nil {
		log.Errorf("DelEndpoint returned %v", err)
	} else {
		log.Infof("EP deleted pod: %s\n", pInfo.Name)
	}
	cmdDel(pInfo.NwNameSpace, pInfo.IntfName)
}

func getPrefixedLogger() *logger.Entry {
	var nsID string

	netNS := os.Getenv("CNI_NETNS")
	ok := strings.HasPrefix(netNS, "/proc/")
	if ok {
		elements := strings.Split(netNS, "/")
		nsID = elements[2]
	} else {
		nsID = "EMPTY"
	}

	l := logger.WithFields(logger.Fields{
		"NETNS": nsID,
	})

	return l
}

func loadConf(bytes []byte) (*types.NetConf, error) {
	n := &types.NetConf{}
	if err := json.Unmarshal(bytes, n); err != nil {
		return nil, log.Errorf("failed to load netconf: %v %q", err, string(bytes))
	}
	return n, nil
}

func saveStdin() ([]byte, error) {
	// Read original stdin
	stdinData, err := ioutil.ReadAll(os.Stdin)
	if err != nil {
		return nil, err
	}

	// Make a new pipe for stdin, and write original stdin data to it
	r, w, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	if _, err := w.Write(stdinData); err != nil {
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}

	os.Stdin = r
	return stdinData, nil
}

func main() {
	var showVersion bool

	// parse rest of the args that require creating state
	flagSet := flag.NewFlagSet("skylarkcni", flag.ExitOnError)

	flagSet.BoolVar(&showVersion,
		"version",
		false,
		"Show version")

	if err := flagSet.Parse(os.Args[1:]); err != nil {
		logger.Fatalf("Failed to parse command. Error: %s", err)
	}
	if showVersion {
		fmt.Printf("1.0.0")
		os.Exit(0)
	}

	mainfunc()
}

func mainfunc() {
	pInfo := cniapi.CNIPodAttr{}
	cniCmd := os.Getenv("CNI_COMMAND")

	// Open a logfile
	f, err := os.OpenFile("/var/log/skylarkcni.log", os.O_RDWR|os.O_CREATE|os.O_APPEND, 0666)
	if err != nil {
		logger.Fatalf("error opening file: %v", err)
	}
	defer f.Close()

	logger.SetOutput(f)
	log = getPrefixedLogger()

	log.Infof("==> Start New Log <==\n")
	log.Infof("command: %s, cni_args: %s", cniCmd, os.Getenv("CNI_ARGS"))

	// Collect information passed by CNI
	err = getPodInfo(&pInfo)
	if err != nil {
		log.Fatalf("Error parsing environment. Err: %v", err)
	}

	//Load network config
	stdinData, err := saveStdin()
	if err != nil {
		log.Errorf("Error read stdin data %v", err)
	}
	netConf, err := loadConf(stdinData)
	if err != nil {
		log.Errorf("Error parse network config %v", err)
	}
	log.Infof("netConf: %s", netConf)

	nc := clients.NewNWClient()
	if cniCmd == "ADD" {
		addPodToNet(nc, &pInfo, netConf)
	} else if cniCmd == "DEL" {
		deletePodFromNet(nc, &pInfo, netConf)
	}

}
