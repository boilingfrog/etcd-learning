package main

import (
	"context"
	"etcd-learning/discovery/helloworld"
)

type Service struct {
}

type server struct {
	service *Service
}

func (s server) SayHello(ctx context.Context, re *helloworld.HelloRequest) (*helloworld.HelloReply, error) {

	return &helloworld.HelloReply{
		Message: "hello " + re.Name,
	}, nil
}
