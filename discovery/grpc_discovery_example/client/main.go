package main

import (
	"etcd-learning/discovery/grpc_discovery_example/server/helloworld"
	"fmt"

	"golang.org/x/net/context"
)

func main() {
	helloClient, err := helloworld.NewClient()
	if err != nil {
		panic(err)
	}
	res, err := helloClient.SayHello(context.Background(), &helloworld.HelloRequest{
		Name: "xiaoming",
	})
	if err != nil {
		fmt.Println(err)
	}
	fmt.Println(res)
}
