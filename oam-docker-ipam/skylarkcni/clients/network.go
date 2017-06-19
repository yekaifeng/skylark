/***
Copyright 2015 Cisco Systems Inc. All rights reserved.

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

package clients

import (
	"bytes"
	"encoding/json"
	"fmt"
	log "github.com/Sirupsen/logrus"
	"oam-docker-ipam/skylarkcni/cniapi"
	"oam-docker-ipam/skylarkcni/ipamapi"
	"github.com/containernetworking/cni/pkg/types"
	"io/ioutil"
	"net"
	"net/http"
	"strings"
)

const (
	nwURL = "http://localhost"
	manifest = `{"Implements": ["IpamDriver"]}`

	capabilitiesPath   = "/IpamDriver.GetCapabilities"
	addressSpacesPath  = "/IpamDriver.GetDefaultAddressSpaces"
	requestPoolPath    = "/IpamDriver.RequestPool"
	releasePoolPath    = "/IpamDriver.ReleasePool"
	requestAddressURL = "/IpamDriver.RequestAddress"
	releaseAddressURL = "/IpamDriver.ReleaseAddress"
	getAddressURL = "/IpamDriver.GetAddress"
)

// NWClient defines informatio needed for the k8s api client
type NWClient struct {
	baseURL string
	client  *http.Client
}

func unixDial(proto, addr string) (conn net.Conn, err error) {
	sock := ipamapi.IpamSocket
	return net.Dial("unix", sock)
}

// NewNWClient creates an instance of the network driver client
func NewNWClient() *NWClient {
	c := NWClient{}
	c.baseURL = nwURL

	transport := &http.Transport{Dial: unixDial}
	c.client = &http.Client{Transport: transport}

	return &c
}

// Request ip address with ipam interface
func (c *NWClient) RequestAddress(podInfo *cniapi.CNIPodAttr, netConf *types.NetConf) (*ipamapi.RequestAddressResponse, error) {
	poolId := strings.Split(netConf.IPAM.Subnet, "/")[0]
	options := map[string]string{"InfraContainerid": podInfo.InfraContainerID}
	req := ipamapi.RequestAddressRequest{PoolID: poolId, Address: nil,
		Options: options}
	res := ipamapi.RequestAddressResponse{}


	buf, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	body := bytes.NewBuffer(buf)
	r, err := c.client.Post(requestAddressURL, "application/json", body)
	if err != nil {
		return nil, err
	}
	defer r.Body.Close()

	switch {
	case r.StatusCode == int(404):
		return nil, fmt.Errorf("Page not found!")

	case r.StatusCode == int(403):
		return nil, fmt.Errorf("Access denied!")

	case r.StatusCode == int(500):
		info, err := ioutil.ReadAll(r.Body)
		if err != nil {
			return nil, err
		}
		err = json.Unmarshal(info, &res)
		if err != nil {
			return nil, err
		}
		return &res, fmt.Errorf("Internal Server Error")

	case r.StatusCode != int(200):
		log.Errorf("POST Status '%s' status code %d \n", r.Status, r.StatusCode)
		return nil, fmt.Errorf("%s", r.Status)
	}

	response, err := ioutil.ReadAll(r.Body)
	if err != nil {
		return nil, err
	}

	err = json.Unmarshal(response, &res)
	if err != nil {
		return nil, err
	}

	return &res, nil
}

// Release IP address from ipam interface
func (c *NWClient) ReleaseAddress(netconf *types.NetConf, ipaddress string) error {
	poolId := strings.Split(netconf.IPAM.Subnet, "/")[0]
	req := ipamapi.ReleaseAddressRequest{PoolID: poolId, Address: ipaddress}
	res := ipamapi.ReleaseAddressResponse{}
	buf, err := json.Marshal(req)
	if err != nil {
		return err
	}

	body := bytes.NewBuffer(buf)
	r, err := c.client.Post(releaseAddressURL, "application/json", body)
	if err != nil {
		return err
	}
	defer r.Body.Close()

	switch {
	case r.StatusCode == int(404):
		return fmt.Errorf("Page not found!")
	case r.StatusCode == int(403):
		return fmt.Errorf("Access denied!")
	case r.StatusCode == int(500):
		info, err := ioutil.ReadAll(r.Body)
		if err != nil {
			return err
		}
		err = json.Unmarshal(info, &res)
		if err != nil {
			return err
		}
		return fmt.Errorf("%v", res)
	case r.StatusCode != int(200):
		log.Errorf("POST Status '%s' status code %d \n", r.Status, r.StatusCode)
		return fmt.Errorf("%s", r.Status)
	}
	response, err := ioutil.ReadAll(r.Body)
	if err != nil {
		return err
	}

	err = json.Unmarshal(response, &res)
	if err != nil {
		return err
	}
	log.Info(res)
	return nil
}

// Hack interface: Query IP address from ipam by infra-container id
func (c *NWClient) GetAddress(infracontainerid string) (string,error) {
	req := ipamapi.GetAddressRequest{ContainerID: infracontainerid}
	res := ipamapi.GetAddressResponse{}
	buf, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	body := bytes.NewBuffer(buf)
	r, err := c.client.Post(getAddressURL, "application/json", body)
	if err != nil {
		return nil, err
	}
	defer r.Body.Close()

	switch {
	case r.StatusCode != int(200):
		log.Errorf("POST Status '%s' status code %d \n", r.Status, r.StatusCode)
		return nil, fmt.Errorf("%s", r.Status)
	}

	response, err := ioutil.ReadAll(r.Body)
	if err != nil {
		return nil, err
	}

	err = json.Unmarshal(response, &res)
	if err != nil {
		return nil, err
	}
        return res.Address, nil

}