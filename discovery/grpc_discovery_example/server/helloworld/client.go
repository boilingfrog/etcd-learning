package helloworld

import (
	"etcd-learning/discovery"

	"go.uber.org/zap"

	grpc "google.golang.org/grpc"
	"google.golang.org/grpc/balancer/roundrobin"
	"google.golang.org/grpc/resolver"
)

const App = "base-hello"

func NewClient(opts ...grpc.DialOption) (GreeterClient, error) {
	options := []grpc.DialOption{
		grpc.WithInsecure(),
		grpc.WithBalancerName(roundrobin.Name),
	}

	addrs := []string{"127.0.0.1:2379"}
	r := discovery.NewResolver(addrs, zap.NewNop())
	resolver.Register(r)

	conn, err := grpc.Dial("etcd:///"+App, options...)
	if err != nil {
		return nil, err
	}
	return NewGreeterClient(conn), nil
}
