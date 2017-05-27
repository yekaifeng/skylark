package db

import (
	"strings"
	"time"
	"errors"

	log "github.com/Sirupsen/logrus"
	"github.com/coreos/etcd/client"
	"golang.org/x/net/context"
)

var db_addr string

func SetDBAddr(addr string) {
	db_addr = addr
}

func GetKey(key string) (string, error) {
	cli := newClient()
	kapi := client.NewKeysAPI(cli)
	resp, err := kapi.Get(context.Background(), key, nil)
	if err != nil {
		log.Error(err)
		return "", err
	} else {
		log.Debugf("Get key %s with value %s", resp.Node.Key, resp.Node.Value)
	}
	return resp.Node.Value, err
}

func GetKeys(dir string) (client.Nodes, error) {
	cli := newClient()
	kapi := client.NewKeysAPI(cli)
	resp, err := kapi.Get(context.Background(), dir, &client.GetOptions{Sort: true})
	if err != nil {
		log.Error(err)
		return nil, err
	} else {
		log.Debugf("Get %d keys from dir %s", len(resp.Node.Nodes), resp.Node.Key)
	}
	return resp.Node.Nodes, err
}

func IsKeyExist(key string) bool {
	cli := newClient()
	kapi := client.NewKeysAPI(cli)
	_, err := kapi.Get(context.Background(), key, nil)
	if client.IsKeyNotFound(err) == true {
		return false
	} else if err != nil {
		log.Fatal(err)
	}
	return true
}

func SetKey(key, value string) error {
	cli := newClient()
	kapi := client.NewKeysAPI(cli)
	resp, err := kapi.Set(context.Background(), key, value, nil)
	if err != nil {
		log.Error(err)
		return err
	} else {
		log.Debugf("Set key %s with value %s", resp.Node.Key, resp.Node.Value)
	}
	return err
}

func SetKeyTTL(key, value string, ttl int) error {
	cli := newClient()
	kapi := client.NewKeysAPI(cli)
	resp, err := kapi.Set(context.Background(), key, value, &client.SetOptions{TTL: time.Duration(ttl)* time.Second})
	if err != nil {
		log.Error(err)
		return err
	} else {
		log.Debugf("Set key %s with value %s", resp.Node.Key, resp.Node.Value, resp.Node.TTL)
	}
	return err
}

func DeleteKey(key string) error {
	cli := newClient()
	kapi := client.NewKeysAPI(cli)
	resp, err := kapi.Delete(context.Background(), key, &client.DeleteOptions{Recursive: true})
	if err != nil {
		log.Error(err)
		return err
	} else {
		log.Debugf("Delete key %s with value %s", resp.Node.Key, resp.Node.Value)
	}
	return err
}

func WatchKey(key string) (client.Watcher, error) {
        cli := newClient()
        kapi := client.NewKeysAPI(cli)
	watcher := kapi.Watcher(key, &client.WatcherOptions{Recursive: true})
	if watcher == nil {
		log.Errorf("Failed to create etcd watcher ")
		return nil, errors.New("Failed to create etcd watcher")
	}
	return watcher, nil
}

func newClient() client.Client {
	parsed_db_addr := strings.Split(db_addr, ",")
	cfg := client.Config{
		Endpoints: parsed_db_addr,
		Transport: client.DefaultTransport,
		// set timeout per request to fail fast when the target endpoint is unavailable
		HeaderTimeoutPerRequest: time.Second,
	}
	c, err := client.New(cfg)
	if err != nil {
		log.Fatal(parsed_db_addr, err)
	}
	return c
}

type EtcdMutexLock struct {
	Name    string
	Expired int64
}

func (mutexLock EtcdMutexLock) Lock() error {
	opts := &client.SetOptions{
		PrevExist: client.PrevNoExist,
		TTL:       time.Duration(mutexLock.Expired) * time.Second}
	cli := newClient()
	kapi := client.NewKeysAPI(cli)
	_, err := kapi.Set(context.TODO(), mutexLock.Name, mutexLock.Name, opts)
	if err != nil {
		return err
	}
	return nil
}

func (mutexLock EtcdMutexLock) Release() error {
	cli := newClient()
	kapi := client.NewKeysAPI(cli)
	_, err := kapi.Delete(context.TODO(), mutexLock.Name, nil)
	if err == nil {
		return nil
	}
	e, ok := err.(client.Error)
	if ok && e.Code == client.ErrorCodeKeyNotFound {
		return nil
	}
	return err
}

func GetEtcdMutexLock(name string, expired int64) *EtcdMutexLock {
	return &EtcdMutexLock{Name: name, Expired: expired}
}