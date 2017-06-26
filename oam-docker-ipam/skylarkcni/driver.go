package main

import (
	"fmt"
	"net"
	"os"
	"runtime"
	"syscall"

	"oam-docker-ipam/skylarkcni/cniapi"
	//"github.com/containernetworking/cni/pkg/skel"
	"github.com/containernetworking/cni/pkg/types"
	"github.com/containernetworking/cni/pkg/types/current"
	//"github.com/containernetworking/cni/pkg/version"
	"github.com/containernetworking/cni/pkg/ip"
	//"github.com/containernetworking/plugins/pkg/ipam"
	"github.com/containernetworking/cni/pkg/ns"
	//"github.com/containernetworking/plugins/pkg/utils"
	"github.com/vishvananda/netlink"

	"strconv"
	"strings"
)

const defaultBrName = "br0"
const defaultMtu = 1500
const haripinMode = false

func init() {
	// this ensures that main runs only on main thread (thread group leader).
	// since namespace ops (unshare, setns) are done for a single thread, we
	// must ensure that the goroutine does not jump from OS thread to thread
	runtime.LockOSThread()
}


func ensureBridgeAddr(br *netlink.Bridge, ipn *net.IPNet, forceAddress bool) error {
	addrs, err := netlink.AddrList(br, syscall.AF_INET)
	if err != nil && err != syscall.ENOENT {
		return fmt.Errorf("could not get list of IP addresses: %v", err)
	}

	// if there're no addresses on the bridge, it's ok -- we'll add one
	if len(addrs) > 0 {
		ipnStr := ipn.String()
		for _, a := range addrs {
			// string comp is actually easiest for doing IPNet comps
			if a.IPNet.String() == ipnStr {
				return nil
			}

			// If forceAddress is set to true then reconfigure IP address otherwise throw error
			if forceAddress {
				if err = deleteBridgeAddr(br, a.IPNet); err != nil {
					return err
				}
			} else {
				return fmt.Errorf("%q already has an IP address different from %v", br.Name, ipn.String())
			}
		}
	}

	addr := &netlink.Addr{IPNet: ipn, Label: ""}
	if err := netlink.AddrAdd(br, addr); err != nil {
		return fmt.Errorf("could not add IP address to %q: %v", br.Name, err)
	}
	return nil
}

func deleteBridgeAddr(br *netlink.Bridge, ipn *net.IPNet) error {
	addr := &netlink.Addr{IPNet: ipn, Label: ""}

	if err := netlink.LinkSetDown(br); err != nil {
		return fmt.Errorf("could not set down bridge %q: %v", br.Name, err)
	}

	if err := netlink.AddrDel(br, addr); err != nil {
		return fmt.Errorf("could not remove IP address from %q: %v", br.Name, err)
	}

	if err := netlink.LinkSetUp(br); err != nil {
		return fmt.Errorf("could not set up bridge %q: %v", br.Name, err)
	}

	return nil
}

func bridgeByName(name string) (*netlink.Bridge, error) {
	l, err := netlink.LinkByName(name)
	if err != nil {
		return nil, fmt.Errorf("could not lookup %q: %v", name, err)
	}
	br, ok := l.(*netlink.Bridge)
	if !ok {
		return nil, fmt.Errorf("%q already exists but is not a bridge", name)
	}
	return br, nil
}

func ensureBridge(brName string, mtu int) (*netlink.Bridge, error) {
	br := &netlink.Bridge{
		LinkAttrs: netlink.LinkAttrs{
			Name: brName,
			MTU:  mtu,
			// Let kernel use default txqueuelen; leaving it unset
			// means 0, and a zero-length TX queue messes up FIFO
			// traffic shapers which use TX queue length as the
			// default packet limit
			TxQLen: -1,
		},
	}

	err := netlink.LinkAdd(br)
	if err != nil && err != syscall.EEXIST {
		return nil, fmt.Errorf("could not add %q: %v", brName, err)
	}

	// Re-fetch link to read all attributes and if it already existed,
	// ensure it's really a bridge with similar configuration
	br, err = bridgeByName(brName)
	if err != nil {
		return nil, err
	}

	if err := netlink.LinkSetUp(br); err != nil {
		return nil, err
	}

	return br, nil
}

func setupVeth(netns ns.NetNS, br *netlink.Bridge, ifName string, mtu int, hairpinMode bool) (*current.Interface, *current.Interface, error) {
	contIface := &current.Interface{}
	hostIface := &current.Interface{}

	err := netns.Do(func(hostNS ns.NetNS) error {
		// create the veth pair in the container and move host end into host netns
		hostVeth, containerVeth, err := ip.SetupVeth(ifName, mtu, hostNS)
		if err != nil {
			return err
		}
		contIface.Name = containerVeth.Name
		contIface.Mac = containerVeth.HardwareAddr.String()
		contIface.Sandbox = netns.Path()
		hostIface.Name = hostVeth.Name
		return nil
	})
	if err != nil {
		return nil, nil, err
	}

	// need to lookup hostVeth again as its index has changed during ns move
	hostVeth, err := netlink.LinkByName(hostIface.Name)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to lookup %q: %v", hostIface.Name, err)
	}
	hostIface.Mac = hostVeth.Attrs().HardwareAddr.String()

	// connect host veth end to the bridge
	if err := netlink.LinkSetMaster(hostVeth, br); err != nil {
		return nil, nil, fmt.Errorf("failed to connect %q to bridge %v: %v", hostVeth.Attrs().Name, br.Attrs().Name, err)
	}

	// set hairpin mode
	if err = netlink.LinkSetHairpin(hostVeth, hairpinMode); err != nil {
		return nil, nil, fmt.Errorf("failed to setup hairpin mode for %v: %v", hostVeth.Attrs().Name, err)
	}

	return hostIface, contIface, nil
}


func calcGatewayIP(ipn *net.IPNet) net.IP {
	nid := ipn.IP.Mask(ipn.Mask)
	return ip.NextIP(nid)
}

func cmdAdd(pInfo *cniapi.CNIPodAttr, netconf *types.NetConf, ipaddress string) error {
	ifname := pInfo.IntfName
	networkns := pInfo.NwNameSpace
	//create bridge if not existed
	br0, err := ensureBridge(defaultBrName, defaultMtu)
	if err != nil {
		log.Errorf("failed to create bridge %s", defaultBrName)
		return err
	}

	//open network namespace
	netns, err := ns.GetNS(networkns)
	if err != nil {
		log.Errorf("failed to open netns %q: %v", networkns, err)
		return err
	}
	defer netns.Close()

	//setup veth pair
	hostInterface, containerInterface, err := setupVeth(netns, br0, ifname, defaultMtu, haripinMode)
	if err != nil {
		log.Errorf("failed to setup veth pair for %s", networkns)
		return err
	}
	log.Infof("Host Interface: %v", hostInterface)
	log.Infof("Container Interface: %v", containerInterface)

	//provision container: ip address, default route, mac address
        if err = netns.Do(func(_ ns.NetNS) error {

		link, err := netlink.LinkByName(ifname)
		if err != nil {
			log.Errorf("failed to lookup %q: %v", ifname, err)
			return err
		}

		if err := netlink.LinkSetUp(link); err != nil {
			return fmt.Errorf("failed to set %q UP: %v", ifname, err)
		}

		//provision ip address
		subnet := netconf.IPAM.Subnet
		s1 := strings.Split(subnet, "/")[1]
		prefix, _ := strconv.Atoi(s1)
		ipaddr := net.IPNet{IP: net.ParseIP(ipaddress), Mask: net.CIDRMask(prefix, net.IPv4len*8)}
		addr := &netlink.Addr{IPNet: &ipaddr, Label: ""}
		if err = netlink.AddrAdd(link, addr); err != nil {
			log.Errorf("failed to add IP addr %v to %q: %v", ipaddr, ifname, err)
			return err
		}

		//provision gateway
		gw := net.ParseIP(netconf.IPAM.Gateway)
		if err = ip.AddDefaultRoute(gw, link); err != nil {
			// we skip over duplicate routes as we assume the first one wins
			if !os.IsExist(err) {
				log.Errorf("failed to add default route %v dev %v': %v", gw, ifname, err)
				return err
			}
		}

		//provision mac address
		if err := ip.SetHWAddrByIP(ifname, ipaddr.IP, nil ); err != nil {
			return err
		}

		// Refetch the veth since its MAC address may changed
		link, err = netlink.LinkByName(ifname)
		if err != nil {
			log.Errorf("could not lookup %q: %v", ifname, err)
			return err
		}

		// Refetch the bridge since its MAC address may change when the first
		// veth is added or after its IP address is set
		br0, err = bridgeByName(defaultBrName)
		if err != nil {
			return err
		}

		log.Infof("Success ADD: %s, %s", networkns, ifname)
		return nil
        }); err != nil {
		return err
	}

        return nil
}

func cmdDel(networkns string, ifname string) error {
	if networkns == "" {
	        return nil
        }

	// There is a netns so try to clean up. Delete can be called multiple times
	// so don't return an error if the device is already removed.
	// If the device isn't there then don't try to clean up IP masq either.
	var ipn *net.IPNet
	err := ns.WithNetNSPath(networkns, func(_ ns.NetNS) error {
		var err error
		ipn, err = ip.DelLinkByNameAddr(ifname, netlink.FAMILY_V4)
		if err != nil && err == ip.ErrLinkNotFound {
			return nil
		}
		return err
	})

	if err != nil {
		return err
	}
        fmt.Println("Success DEL: %s, %s", networkns, ifname)
	return nil
}




