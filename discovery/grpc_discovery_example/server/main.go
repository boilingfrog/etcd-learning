package main

import (
	"etcd-learning/discovery"
	"etcd-learning/discovery/helloworld"
	"fmt"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"google.golang.org/grpc"

	"go.uber.org/zap"
)

const (
	app         = "base-hello"
	grpcAddress = "127.0.0.1:8083"
)

func main() {
	addrs := []string{"127.0.0.1:2379"}
	etcdRegister := discovery.NewRegister(addrs, zap.NewNop())
	node := discovery.Server{
		Name: app,
		Addr: grpcAddress,
	}

	server, err := Start()
	if err != nil {
		panic(fmt.Sprintf("start server failed : %v", err))
	}

	if _, err := etcdRegister.Register(node, 10); err != nil {
		panic(fmt.Sprintf("server register failed: %v", err))
	}

	fmt.Println("service started listen on", grpcAddress)
	c := make(chan os.Signal, 1)
	signal.Notify(c, syscall.SIGHUP, syscall.SIGQUIT, syscall.SIGTERM, syscall.SIGINT)
	for {
		s := <-c
		switch s {
		case syscall.SIGQUIT, syscall.SIGTERM, syscall.SIGINT:
			server.Stop()
			etcdRegister.Stop()
			time.Sleep(time.Second)
			return
		case syscall.SIGHUP:
		default:
			return
		}
	}
}

func Start() (*grpc.Server, error) {
	s := grpc.NewServer()

	helloworld.RegisterGreeterServer(s, &server{})
	lis, err := net.Listen("tcp", grpcAddress)
	if err != nil {
		return nil, err
	}

	go func() {
		if err := s.Serve(lis); err != nil {
			panic(err)
		}
	}()

	return s, nil
}
