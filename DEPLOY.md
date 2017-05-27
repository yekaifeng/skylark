# skylark 部署

![Deply Logo](doc/skylark-deploy.png?raw=true "Deploy Logo")


## 版本依赖

* RHEL/CentOS 7.2
* docker-engine 1.11.2
* etcd 大于 2.2.5

## 部署过程

### 一. 配置br0网桥，删除原的docker0配置（如有必要）,重启机器使配置生效
以下面为例,bond0上配了vlan 52, 并加入br0网桥

    [root@mesos-slave-01]# cat /etc/sysconfig/network-scripts/ifcfg-bond0
    DEVICE=bond0
    NAME=bond0
    TYPE=Bond
    BONDING_MASTER=yes
    ONBOOT=yes
    BOOTPROTO=none
    BONDING_OPTS="miimon=100 mode=4 lacp_rate=fast xmit_hash_policy=layer2+3"
    
    [root@mesos-slave-01s]# cat /etc/sysconfig/network-scripts/ifcfg-bond0.52
    VLAN=yes
    DEVICE=bond0.52
    NAME=bond0.52
    PHYSDEV=bond0
    ONBOOT=yes
    BOOTPROTO=none
    ONPARENT=yes
    BRIDGE=br0
    NM_CONTROLLED=no
    
    [root@mesos-slave-01]# cat /etc/sysconfig/network-scripts/ifcfg-br0
    DEVICE=br0
    ONBOOT=yes
    TYPE=Bridge
    IPADDR=10.190.52.34
    NETMASK=255.255.255.0
    NM_CONTROLLED=no

    # mv ifcfg-docker0 bak_ifcfg-docker0
    重启主机，以保证旧有的docker0不再占有10.190.52.0/24网段

### 二.安装 etcd（三个节点）

    # yum install -y etcd
    # cat /etc/etcd/etcd.conf 
    #[member]
    ETCD_NAME=mesos-slave-01
    ETCD_DATA_DIR="/var/lib/etcd/default.etcd"
    #ETCD_WAL_DIR=""
    #ETCD_SNAPSHOT_COUNT="10000"
    #ETCD_HEARTBEAT_INTERVAL="100"
    #ETCD_ELECTION_TIMEOUT="1000"
    ETCD_LISTEN_PEER_URLS="http://10.190.51.34:2380"
    ETCD_LISTEN_CLIENT_URLS="http://10.190.51.34:2379,http://127.0.0.1:2379"
    #ETCD_MAX_SNAPSHOTS="5"
    #ETCD_MAX_WALS="5"
    #ETCD_CORS=""
    #
    #[cluster]
    ETCD_INITIAL_ADVERTISE_PEER_URLS="http://10.190.51.34:2380"
    # if you use different ETCD_NAME (e.g. test), set ETCD_INITIAL_CLUSTER value for this name, i.e. "test=http://..."
    ETCD_INITIAL_CLUSTER="mesos-slave-01=http://10.190.51.34:2380,mesos-slave-02=http://10.190.51.35:2380,mesos-slave-03=http://10.190.51.36:2380"
    ETCD_INITIAL_CLUSTER_STATE="new"
    ETCD_INITIAL_CLUSTER_TOKEN="etcd-cluster"
    ETCD_ADVERTISE_CLIENT_URLS="http://10.190.51.34:2379"
    
    # systemctl start etcd
    # systemctl enable etcd

### 三.安装,设置并启动oam-docker-ipam服务

#### 安装oam-docker-ipam
    # yum localinstall ./yum localinstall -y oam-docker-ipam-1.0.4-1.el7.centos.x86_64.rpm

#### 启动服务
    [root@mesos-slave-01 ~]# systemctl start oam-docker-ipam
    [root@mesos-slave-01 ~]# systemctl enable oam-docker-ipam
    [root@mesos-slave-01 ~]# systemctl status oam-docker-ipam
    ● oam-docker-ipam.service - oam-docker-ipam
       Loaded: loaded (/usr/lib/systemd/system/oam-docker-ipam.service; disabled; vendor preset: disabled)
       Active: active (running) since Tue 2016-11-08 09:53:51 CST; 6s ago
     Main PID: 11423 (oam-docker-ipam)
       Memory: 1.5M
       CGroup: /system.slice/oam-docker-ipam.service
               └─11423 /usr/bin/oam-docker-ipam --debug=true --cluster-store=http://10.190.51.34:2379,http://10.19...

### 四.连接etcd创建IP地址池

    根据实际需要修改etcd的连接信息
    [root@mesos-slave-01 ~]# oam-docker-ipam --cluster-store "http://127.0.0.1:2379" ip-range --ip-start 10.0.2.100/24 --ip-end 10.0.2.200/24


### 五.创建网络mynet(所有宿主机都要跑一次)

    #!/bin/bash -x
    
    gw=$(ip addr show br0|grep inet|awk '{{print $2}}'|cut -d '/' -f 1)
    docker network create \
    --opt=com.docker.network.bridge.enable_icc=true \
    --opt=com.docker.network.bridge.enable_ip_masquerade=false \
    --opt=com.docker.network.bridge.host_binding_ipv4=0.0.0.0 \
    --opt=com.docker.network.bridge.name=br0 \
    --opt=com.docker.network.driver.mtu=1500 \
    --ipam-driver=skylark \
    --subnet=10.190.52.0/24 \
    --gateway=$gw \
    --aux-address=DefaultGatewayIPv4=10.190.52.1 \
    mynet

    # --gateway 为br0 interface的IP地址, 不同宿主机这个地址是不一样的，可用这个命令查看ip addr show br0
    # --aux-address 为所有容器的统一gateway

### 六.使用mynet跑一个容器

    [root@mesos-slave-01 ~]# docker run -d --name a1 --net mynet centos:7 /bin/bash -c 'while true;do echo test;sleep 90;done'
    979600195a15460371c222827c275938368f1a18131dca591ce35c320ee4c701
    