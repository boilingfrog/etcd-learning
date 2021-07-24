package cache

import (
	"context"
	"fmt"
	"time"

	"go.uber.org/zap"

	"go.etcd.io/etcd/api/v3/mvccpb"
	clientv3 "go.etcd.io/etcd/client/v3"
)

func init() {
	handleMap = make(map[string]func([]byte) error)
}

var handleMap map[string]func([]byte) error

/*
RegisterUpdateHandle
Must Be Register handle if pub to client
*/
func RegisterUpdateHandle(key string, f func([]byte) error) {
	handleMap[key] = f
}

type PubClient interface {
	Pub(ctx context.Context, key string, val string) error
}

var Pub PubClient

type PubClientImpl struct {
	client *clientv3.Client
	logger *zap.Logger
	prefix string
}

func (c *PubClientImpl) Watcher() {
	ctx, cancel := context.WithCancel(context.Background())
	rch := c.client.Watch(ctx, c.prefix, clientv3.WithPrefix())
	defer cancel()

	for wresp := range rch {
		for _, ev := range wresp.Events {
			switch ev.Type {
			case mvccpb.PUT:
				c.logger.Warn("Cache Update", zap.Any("value", ev.Kv))
				err := handleCacheUpdate(ev.Kv)
				if err != nil {
					c.logger.Error("Cache Update", zap.Error(err))
				}
			case mvccpb.DELETE:
				c.logger.Error("Cache Delete NOT SUPPORT")
			}
		}
	}
}

func handleCacheUpdate(val *mvccpb.KeyValue) error {
	if val == nil {
		return nil
	}
	f := handleMap[string(val.Key)]
	if f != nil {
		return f(val.Value)
	}
	return nil
}

func (c *PubClientImpl) Pub(ctx context.Context, key string, val string) error {
	ctx, _ = context.WithTimeout(ctx, time.Second*10)
	_, err := c.client.Put(ctx, key, val)
	if err != nil {
		return err
	}
	return nil
}

func NewPubClient(config clientv3.Config, prefix string, logger *zap.Logger) (err error) {

	logger.Info("Pub Etcd Connection", zap.Any("config", config), zap.String("prefix", prefix))
	client, err := clientv3.New(config)
	if err != nil {
		return err
	}

	pci := &PubClientImpl{
		client: client,
		logger: logger,
		prefix: prefix,
	}
	go func() {
		pci.Watcher()
	}()

	Pub = pci
	return nil
}

type FakePubClient struct {
}

func (c *FakePubClient) Pub(ctx context.Context, key string, val string) error {

	fmt.Printf("Pub Key %s, Val %s \n", key, val)
	return nil
}

func NewTestPubClient() {
	Pub = &FakePubClient{}
}
