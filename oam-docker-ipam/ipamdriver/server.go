package ipamdriver

import (
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"os"
	"net"
	"time"
	"os/exec"
	"strconv"
	"math/rand"
	"regexp"

	log "github.com/Sirupsen/logrus"
	"github.com/docker/go-plugins-helpers/ipam"
	"golang.org/x/net/context"
	etcdclient "github.com/coreos/etcd/client"
	"github.com/docker/engine-api/client"
	"github.com/docker/engine-api/types"

	"oam-docker-ipam/db"
	"oam-docker-ipam/util"
)

const (
	network_key_prefix = "/skylark/networks"
	pod_key_prefix = "/skylark/pods"
)

var hostname string
var byteResps = make(chan [2][]byte, 1)

type Config struct {
	Ipnet string
	Mask  string
}

func StartServer() {
	hostname = GetHostName()
	log.Infof("Server start with hostname: %s", hostname)
	//Release the ip resources that occupied by dead containers in localhost
	go IpResourceCleanUP()

	go handleChannelEvent(byteResps)
	//Create etcd watcher and event handler for rate limit change
	watcher, err := db.WatchKey(network_key_prefix)
	if err != nil {
		log.Errorf("error to create etcd watcher")
	} else {
		go receiveEtcdEvents(watcher, byteResps)
	}

	d := &MyIPAMHandler{}
	h := ipam.NewHandler(d)
	h.ServeUnix("root", "skylark")
}

func AllocateIPRange(ip_start, ip_end string) []string {
	ips := util.GetIPRange(ip_start, ip_end)
	ip_net, mask := util.GetIPNetAndMask(ip_start)
	for _, ip := range ips {
		if checkIPAssigned(ip_net, ip) {
			log.Warnf("IP %s has been allocated", ip)
			continue
		}
		db.SetKey(filepath.Join(network_key_prefix, ip_net, "pool", ip), "")
	}
	initializeConfig(ip_net, mask)
	fmt.Println("Allocate Containers IP Done! Total:", len(ips))
	return ips
}

func ReleaseIP(ip_net, ip string) error {
	value, _ := db.GetKey(filepath.Join(network_key_prefix, ip_net, "assigned", hostname, ip))
	if value != nil {
		DeleteEndpointFromStore(value)
	}

	err := db.DeleteKey(filepath.Join(network_key_prefix, ip_net, "assigned", hostname, ip))
	if err != nil {
		log.Infof("Skip Release IP %s", ip)
		return nil
	}
	err = db.SetKey(filepath.Join(network_key_prefix, ip_net, "pool", ip), "")
	if err == nil {
		log.Infof("Release IP %s", ip)
	}
	return nil
}

func AllocateIP(ip_net, ip string) (string, error) {
        // create a lock
	lock := db.GetEtcdMutexLock(filepath.Join(network_key_prefix, ip_net, "wait"), 20)
        log.Debugf("Lock instance:%s", lock)

        err := lock.Lock()

	var cnt int = 0
	if err != nil {
		// if locked by others, wait for 30 times with random 900 mil-sec interval.
		for {
			cnt = cnt + 1
			time.Sleep(time.Duration(rand.Intn(900)) * time.Millisecond)
			log.Debugf("Locked by others, %d retry ...", cnt)
			e := lock.Lock()
			if e == nil {
				ip, err := getIP(ip_net, ip)
				lock.Release()
				return ip, err
				break
			}
			if cnt > 30 {
				log.Debugf("Abort ...")
				break
			}
		}
	} else {
		// if lock successfully, go ahead to acquire the ip
		ip, err := getIP(ip_net, ip)
		lock.Release()
		return ip, err
	}
        return "", errors.New("Can not allocate ip")
}

func getIP(ip_net, ip string) (string, error) {
	ip_pool, err := db.GetKeys(filepath.Join(network_key_prefix, ip_net, "pool"))
	if err != nil {
		return ip, err
	}
	if len(ip_pool) == 0 {
		return ip, errors.New("Pool is empty")
	}
	if ip == "" {
		find_ip := strings.Split(ip_pool[0].Key, "/")
		ip = find_ip[len(find_ip) - 1]
	}
	exist := checkIPAssigned(ip_net, ip)
	if exist == true {
		return ip, errors.New(fmt.Sprintf("IP %s has been allocated", ip))
	}
	err = db.DeleteKey(filepath.Join(network_key_prefix, ip_net, "pool", ip))
	if err != nil {
		return ip, err
	}
	db.SetKey(filepath.Join(network_key_prefix, ip_net, "assigned", hostname, ip), "")
	log.Infof("Allocated IP %s", ip)

	//query container env, save flow limit setting into the kv store if available
	go updateFlowLimit(ip_net, ip)
	return ip, err
}

func checkIPAssigned(ip_net, ip string) bool {
	if exist := db.IsKeyExist(filepath.Join(network_key_prefix, ip_net, "assigned", hostname, ip)); exist {
		return true
	}
	return false
}

func initializeConfig(ip_net, mask string) error {
	config := &Config{Ipnet: ip_net, Mask: mask}
	config_bytes, err := json.Marshal(config)
	if err != nil {
		log.Fatal(err)
	}
	err = db.SetKey(filepath.Join(network_key_prefix, ip_net, "config"), string(config_bytes))
	if err == nil {
		log.Infof("Initialized Config %s for network %s", string(config_bytes), ip_net)
	}
	return err
}

func DeleteNetWork(ip_net string) error {
	err := db.DeleteKey(filepath.Join(network_key_prefix, ip_net))
	if err == nil {
		log.Infof("DeleteNetwork %s", ip_net)
	}
	return err
}

func GetConfig(ip_net string) (*Config, error) {
	config, err := db.GetKey(filepath.Join(network_key_prefix, ip_net, "config"))
	if err == nil {
		log.Debugf("GetConfig %s from network %s", config, ip_net)
	}
	conf := &Config{}
	json.Unmarshal([]byte(config), conf)
	return conf, err
}

func GetHostName() string {
	hostname, err := os.Hostname()
	if err != nil {
		log.Fatalf("Could not retrieve hostname: %v", err)
	}
	return hostname
}

func IpResourceCleanUP() {
	// get all the ip subnet name to the slice
	log.Info("start to clean up ip resource in current host ...")
	var subnets []string
        ip_net, _ := db.GetKeys(network_key_prefix)
        if len(ip_net) != 0 {
		for _, n := range ip_net {
			find_net_str := strings.Split(n.Key, "/")
			subnets = append(subnets, find_net_str[len(find_net_str)-1])
		}
	        log.Info("subnet info:", subnets)
	}

        var host_assinged_ips []string
        for _, subnet := range subnets{
		key_assigned := filepath.Join(network_key_prefix, subnet, "assigned", hostname)
		if db.IsKeyExist(key_assigned) {
			// get all the assigned ips in current host
			ip_assigned_keys, _ := db.GetKeys(key_assigned)
			for _, ip := range ip_assigned_keys {
				find_ip_str := strings.Split(ip.Key, "/")
				host_assinged_ips = append(host_assinged_ips, find_ip_str[len(find_ip_str) - 1])
			}
			log.Info("subnet ip keys:", ip_assigned_keys)
		}
	}
	log.Info("assigeded ips in this host:", host_assinged_ips)

	// get all the active ips in this host
	var active_ips []string
	containers, _ := ListContainers("unix:///var/run/docker.sock")
        for _,container := range containers {
	    // get network setting in each container
	    networks := container.NetworkSettings.Networks
		for _,n := range networks {
			log.Info("Found active IP:", n.IPAddress)
			active_ips = append(active_ips, n.IPAddress)
		}
	}
	log.Info("active IPs:", active_ips)

	// find residual ips and clean up
        var found bool = false
        for _, ip := range host_assinged_ips {
		for _, active_ip := range active_ips {
			if ip == active_ip {
				// this is active
                                found = true
			}
		}
		//if no active ip found, clean up this ip
		if found == false {
			for _,subnet := range subnets {
				key_to_delete := filepath.Join(network_key_prefix, subnet, "assigned", hostname, ip)
				if db.IsKeyExist(key_to_delete){
					ReleaseIP(subnet, ip)
				}
			}
		}
		found = false
	}

}

func ListContainers(socketurl string) ([]types.Container, error) {
	var c *client.Client
	var err error
	defaultHeaders := map[string]string{"User-Agent": "engine-api-cli-1.0"}
	c,err = client.NewClient(socketurl, "", nil, defaultHeaders)
	if err != nil {
		log.Fatalf("Create Docker Client error", err)
		return nil, err
	}

	// List containers
	opts := types.ContainerListOptions{}
	ctx, cancel := context.WithTimeout(context.Background(), 20000*time.Millisecond)
	defer cancel()
	containers, err := c.ContainerList(ctx, opts)
	if err != nil {
		log.Fatal("List Container error", err)
		return nil, err
	}
	return containers, err
}

func InspectContainer(socketurl string, id string) (types.ContainerJSON, error) {
	var c *client.Client
	var err error
	defaultHeaders := map[string]string{"User-Agent": "engine-api-cli-1.0"}
	c,err = client.NewClient(socketurl, "", nil, defaultHeaders)
	if err != nil {
		log.Fatalf("Create Docker Client error", err)
		return types.ContainerJSON{}, err
	}

	// Inspect container
	ctx, cancel := context.WithTimeout(context.Background(), 2000*time.Millisecond)
	defer cancel()
	containerJson, err := c.ContainerInspect(ctx, id)
	if err != nil {
		log.Fatal("Inspect Container error: %s", id)
		return types.ContainerJSON{}, err
	}
	return containerJson, err

}

func updateFlowLimit(ip_net string, ip string){
	// wait for container creation finished otherwise it will be blocked
	time.Sleep(time.Second)
	target_ip := ip
	var envstr string
	var tmp string
	var limitstr string
	containers, _ := ListContainers("unix:///var/run/docker.sock")
	for _,container := range containers {
		// get network setting in each container
		networks := container.NetworkSettings.Networks
		for _,v := range networks {
			if v.IPAddress == target_ip {
				containerJson, _ := InspectContainer("unix:///var/run/docker.sock",container.ID)

				//get pid and env of target container
				pid := containerJson.State.Pid
				env := containerJson.Config.Env
				//get the env string and put into a map of env
				if len(env) !=0 {
					//get limit setting from env and set flow control
					for _,s := range env {
						s1 := strings.ToUpper(s)
						if strings.HasPrefix(s1, "IN=") || strings.HasPrefix(s1, "OUT="){
							s2 := strings.Split(s1, "=")
							tmp = tmp + "\"" + s2[0] + "\"" + ":" + s2[1] + ","
						}
					}

					//if no limit setting in env set as no flow control
					if tmp == "" {
						limitstr = "\"IN\":0,\"OUT\":0"
					} else {
						limitstr = strings.TrimRight(tmp, ",")
					}
					envstr = strings.Join([]string{"{", limitstr, "}"}, "")
					log.Debug("Pid: ", pid)
					log.Debug("Env: ", env)
					log.Debug("envstr: ", envstr)
				} else {
					//if no env set default to no flow control
					envstr = "{\"IN\":0,\"OUT\":0}"
					//set flow control to zero
					log.Debug("no env found for container: ", container.ID)
				}
				db.SetKey(filepath.Join(network_key_prefix, ip_net, "assigned", hostname, ip), envstr)
			}
		}
	}
}

func receiveEtcdEvents(watcher etcdclient.Watcher, rsps chan [2][]byte) {
	for {
		// block on change notifications
		etcdRsp, err := watcher.Next(context.Background())
		if err != nil {
			log.Errorf("Error %v during watch", err)
			time.Sleep(time.Second)
		}

		hostname,_ := os.Hostname()
		if strings.Contains(etcdRsp.Node.Key, hostname) == false {
			log.Debug("Key not for current host ...")
			continue
		}

		//rsp[0] is current key, rsp[1] is current value
		rsp := [2][]byte{nil, nil}
		eventStr := "create"
		if etcdRsp.Node.Value != "" {
			log.Debug("Current Key: ", etcdRsp.Node.Key)
			log.Debug("Current Value: ", etcdRsp.Node.Value)
			rsp[0] = []byte(etcdRsp.Node.Key)
			rsp[1] = []byte(etcdRsp.Node.Value)
		}
		if etcdRsp.PrevNode != nil && etcdRsp.PrevNode.Value != "" {
			log.Debug("Pre Value: ", etcdRsp.PrevNode.Value)
			if etcdRsp.Node.Value != "" {
				eventStr = "modify"
			} else {
				eventStr = "delete"
			}
		}
		log.Debug("Etcd event occured: ", eventStr)
		rsps <- rsp
	}
}

func handleChannelEvent(byteRsps chan [2][]byte) {
	var limit map[string]int
	for {
		byteRsp := <-byteResps
		key := string(byteRsp[0][:])
		//convert the flow limit from string to map
		json.Unmarshal(byteRsp[1], &limit)

		//get the ip addr in key like /skylark/containers/10.0.2.0/assigned/skylark-1/10.0.2.103
		keyslice := strings.Split(key, "/")
		target_ip := keyslice[len(keyslice)-1]
                valid_ip := net.ParseIP(target_ip)
		if valid_ip == nil {
			log.Debug("invalid ip addr: ", target_ip)
			continue
		}

		//if both IN and OUT are 0, then no limit is set
		if limit["IN"] !=0 && limit["OUT"] !=0 {
			//get corresponding container who owns the target_ip
			containers, _ := ListContainers("unix:///var/run/docker.sock")
			for _, container := range containers {
				// get network setting in each container
				networks := container.NetworkSettings.Networks
				for _, v := range networks {
					if v.IPAddress == target_ip {
						containerJson, _ := InspectContainer("unix:///var/run/docker.sock", container.ID)

						//get pid of target container
						pid := containerJson.State.Pid

						//set flow limit
						go set_flow_limit(pid, limit)
						log.Debug(pid, ":", limit)
					}
				}
			}
		} else {
			log.Debug("No flow limit is required")
			continue
		}

	}
}

func set_flow_limit(pid int, limit map[string]int) {
	in, foundin := limit["IN"]
	out, foundout := limit["OUT"]
	var dev_substr string
	if foundin && foundout {
		host_veth_num := get_host_veth_num(pid, "eth0")

		//search for corresponding veth device in host
		cmd := exec.Command("ip", "link")
		log.Debug(cmd)
		output, err := cmd.CombinedOutput()
		if err != nil {
			log.Error(output)
			return
		}
		iplinks := string(output)

		//find the match for like 5: veth25c84ba@if4: <BROADCAST ...
		linkslice := strings.Split(iplinks, "\n")
		reg := regexp.MustCompile("^" + host_veth_num + ":")
		for _,s := range linkslice {
			if reg.MatchString(s) {
				dev_substr = s
			}
		}

		trim_left := strings.TrimLeft(dev_substr, host_veth_num+": ")
		trim_right_index := strings.Index(trim_left, "@")
		trim_right := trim_left[:trim_right_index]
		host_veth_dev := trim_right
		log.Debug("Target host device: ", host_veth_dev)

		//set the flow limit with wondershaper
		cmd = exec.Command("/sbin/wondershaper", host_veth_dev,
			strconv.Itoa(out), strconv.Itoa(in))
		log.Debug(cmd)
		output, err = cmd.CombinedOutput()
		if err != nil {
			log.Error(output)
			return
		}

		//show the flow limit result
		cmd = exec.Command("/sbin/tc", "qdisc", "show")
		log.Debug(cmd)
		output, err = cmd.CombinedOutput()
		if err != nil {
			log.Error(output)
			return
		}
                log.Debug(string(output))
		log.Debug("Flow limit setting successful!")
		log.Debug("Pid: ", pid)
		log.Debug("Limit: ", limit)
	} else {
		log.Error("Flow limit parameter incomplete!")
	}

}

//this function relay on nsenter tool
func get_host_veth_num(pid int, device string) string {
        //invoke nsenter
	cmd := "nsenter"
	args := fmt.Sprint("-t ",
	                   strconv.Itoa(pid)+ " ",
	                   "-n ",
	                   "ip link show " + device)
	log.Debug(cmd)
	output, err := exec.Command(cmd, strings.Split(args, " ")...).CombinedOutput()
	if err != nil {
		log.Error(output)
		return ""
	}
	//output sth like "37256: eth0@if37257: <BROADCAST,MULTICAST,UP,LOWER_UP>"

	//get substring about ethif
	ethif_slice := strings.Split(string(output), " ")[1]
	ethif := strings.TrimRight(ethif_slice, ":")
	ifnum := strings.TrimLeft(ethif, device+"@if")
	log.Debug("Got veth number: ", ifnum)
	return ifnum
}

func SaveEndpointToStore(infracontainerid string, ip_net string, ip string) error{
	//update container id to ip key
	db.SetKey(filepath.Join(network_key_prefix, ip_net, "assigned", hostname, ip), infracontainerid)
	log.Infof("Complete set value for %s", ip)

	//save pod endpoint info
	err := db.SetKey(filepath.Join(pod_key_prefix, infracontainerid), ip)
	if err != nil {
		log.Errorf("error saving endpoint %s", infracontainerid)
		return err
	}
	return nil
}

func DeleteEndpointFromStore(infracontainerid string) error{
	err := db.DeleteKey(filepath.Join(pod_key_prefix, infracontainerid))
	if err != nil {
		log.Errorf("error deleting endpoint %s", infracontainerid)
		return err
	}
	return nil
}

func GetEndpointFromStore(infracontainerid string) (string, bool) {
	ip, err := db.GetKey(filepath.Join(pod_key_prefix, infracontainerid))
	if err != nil {
		log.Infof("endpoint not found %s", infracontainerid)
		return nil, false
	}
	return ip, true
}
