<!-- START doctoc generated TOC please keep comment here to allow auto update -->
<!-- DON'T EDIT THIS SECTION, INSTEAD RE-RUN doctoc TO UPDATE -->

- [etcd服务发现源码实现](#etcd%E6%9C%8D%E5%8A%A1%E5%8F%91%E7%8E%B0%E6%BA%90%E7%A0%81%E5%AE%9E%E7%8E%B0)
  - [前言](#%E5%89%8D%E8%A8%80)
  - [服务注册](#%E6%9C%8D%E5%8A%A1%E6%B3%A8%E5%86%8C)
  - [服务发现](#%E6%9C%8D%E5%8A%A1%E5%8F%91%E7%8E%B0)
  - [负载均衡](#%E8%B4%9F%E8%BD%BD%E5%9D%87%E8%A1%A1)
    - [集中式LB（Proxy Model）](#%E9%9B%86%E4%B8%AD%E5%BC%8Flbproxy-model)
    - [进程内LB（Balancing-aware Client）](#%E8%BF%9B%E7%A8%8B%E5%86%85lbbalancing-aware-client)
    - [独立 LB 进程（External Load Balancing Service）](#%E7%8B%AC%E7%AB%8B-lb-%E8%BF%9B%E7%A8%8Bexternal-load-balancing-service)
  - [参考](#%E5%8F%82%E8%80%83)

<!-- END doctoc generated TOC please keep comment here to allow auto update -->

## etcd服务发现源码实现

### 前言

项目中使用etcd实现了grpc的服务户注册和服务发现，这里来看下如何实现的服务注册和服务发现  

先来看下使用的demo，demo中的代码[discovery](https://github.com/boilingfrog/etcd-learning/tree/main/discovery)

### 服务注册  

```go
package discovery

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"
	"go.uber.org/zap"
)

// Register for grpc server
type Register struct {
	EtcdAddrs   []string
	DialTimeout int

	closeCh     chan struct{}
	leasesID    clientv3.LeaseID
	keepAliveCh <-chan *clientv3.LeaseKeepAliveResponse

	srvInfo Server
	srvTTL  int64
	cli     *clientv3.Client
	logger  *zap.Logger
}

// NewRegister create a register base on etcd
func NewRegister(etcdAddrs []string, logger *zap.Logger) *Register {
	return &Register{
		EtcdAddrs:   etcdAddrs,
		DialTimeout: 3,
		logger:      logger,
	}
}

// Register a service
func (r *Register) Register(srvInfo Server, ttl int64) (chan<- struct{}, error) {
	var err error

	if strings.Split(srvInfo.Addr, ":")[0] == "" {
		return nil, errors.New("invalid ip")
	}

	if r.cli, err = clientv3.New(clientv3.Config{
		Endpoints:   r.EtcdAddrs,
		DialTimeout: time.Duration(r.DialTimeout) * time.Second,
	}); err != nil {
		return nil, err
	}

	r.srvInfo = srvInfo
	r.srvTTL = ttl

	if err = r.register(); err != nil {
		return nil, err
	}

	r.closeCh = make(chan struct{})

	go r.keepAlive()

	return r.closeCh, nil
}

// Stop stop register
func (r *Register) Stop() {
	r.closeCh <- struct{}{}
}

// register 注册节点
func (r *Register) register() error {
	leaseCtx, cancel := context.WithTimeout(context.Background(), time.Duration(r.DialTimeout)*time.Second)
	defer cancel()

	leaseResp, err := r.cli.Grant(leaseCtx, r.srvTTL)
	if err != nil {
		return err
	}
	r.leasesID = leaseResp.ID
	if r.keepAliveCh, err = r.cli.KeepAlive(context.Background(), leaseResp.ID); err != nil {
		return err
	}

	data, err := json.Marshal(r.srvInfo)
	if err != nil {
		return err
	}
	_, err = r.cli.Put(context.Background(), BuildRegPath(r.srvInfo), string(data), clientv3.WithLease(r.leasesID))
	return err
}

// unregister 删除节点
func (r *Register) unregister() error {
	_, err := r.cli.Delete(context.Background(), BuildRegPath(r.srvInfo))
	return err
}

// keepAlive
func (r *Register) keepAlive() {
	ticker := time.NewTicker(time.Duration(r.srvTTL) * time.Second)
	for {
		select {
		case <-r.closeCh:
			if err := r.unregister(); err != nil {
				r.logger.Error("unregister failed", zap.Error(err))
			}
			if _, err := r.cli.Revoke(context.Background(), r.leasesID); err != nil {
				r.logger.Error("revoke failed", zap.Error(err))
			}
			return
		case res := <-r.keepAliveCh:
			if res == nil {
				if err := r.register(); err != nil {
					r.logger.Error("register failed", zap.Error(err))
				}
			}
		case <-ticker.C:
			if r.keepAliveCh == nil {
				if err := r.register(); err != nil {
					r.logger.Error("register failed", zap.Error(err))
				}
			}
		}
	}
}

// UpdateHandler return http handler
func (r *Register) UpdateHandler() http.HandlerFunc {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		wi := req.URL.Query().Get("weight")
		weight, err := strconv.Atoi(wi)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(err.Error()))
			return
		}

		var update = func() error {
			r.srvInfo.Weight = int64(weight)
			data, err := json.Marshal(r.srvInfo)
			if err != nil {
				return err
			}
			_, err = r.cli.Put(context.Background(), BuildRegPath(r.srvInfo), string(data), clientv3.WithLease(r.leasesID))
			return err
		}

		if err := update(); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(err.Error()))
			return
		}
		w.Write([]byte("update server weight success"))
	})
}

func (r *Register) GetServerInfo() (Server, error) {
	resp, err := r.cli.Get(context.Background(), BuildRegPath(r.srvInfo))
	if err != nil {
		return r.srvInfo, err
	}
	info := Server{}
	if resp.Count >= 1 {
		if err := json.Unmarshal(resp.Kvs[0].Value, &info); err != nil {
			return info, err
		}
	}
	return info, nil
}
```

来分析下上面的代码实现  

当启动一个grpc的时候我们注册到etcd中

```go
	etcdRegister := discovery.NewRegister(config.Etcd.Addrs, log.Logger)
	node := discovery.Server{
		Name: app,
		Addr: utils.InternalIP() + config.Port.GRPC,
	}

	if _, err := etcdRegister.Register(node, 10); err != nil {
		panic(fmt.Sprintf("server register failed: %v", err))
	}
```

调用服务注册的时候首先分配了一个租约  

```go
func (l *lessor) Grant(ctx context.Context, ttl int64) (*LeaseGrantResponse, error) {
	r := &pb.LeaseGrantRequest{TTL: ttl}
	resp, err := l.remote.LeaseGrant(ctx, r, l.callOpts...)
	if err == nil {
		gresp := &LeaseGrantResponse{
			ResponseHeader: resp.GetHeader(),
			ID:             LeaseID(resp.ID),
			TTL:            resp.TTL,
			Error:          resp.Error,
		}
		return gresp, nil
	}
	return nil, toErr(ctx, err)
}
```

然后通过KeepAlive保活   

```go
// KeepAlive尝试保持给定的租约永久alive
func (l *lessor) KeepAlive(ctx context.Context, id LeaseID) (<-chan *LeaseKeepAliveResponse, error) {
	ch := make(chan *LeaseKeepAliveResponse, LeaseResponseChSize)

	l.mu.Lock()
	// ensure that recvKeepAliveLoop is still running
	select {
	case <-l.donec:
		err := l.loopErr
		l.mu.Unlock()
		close(ch)
		return ch, ErrKeepAliveHalted{Reason: err}
	default:
	}
	ka, ok := l.keepAlives[id]
	if !ok {
		// create fresh keep alive
		ka = &keepAlive{
			chs:           []chan<- *LeaseKeepAliveResponse{ch},
			ctxs:          []context.Context{ctx},
			deadline:      time.Now().Add(l.firstKeepAliveTimeout),
			nextKeepAlive: time.Now(),
			donec:         make(chan struct{}),
		}
		l.keepAlives[id] = ka
	} else {
		// add channel and context to existing keep alive
		ka.ctxs = append(ka.ctxs, ctx)
		ka.chs = append(ka.chs, ch)
	}
	l.mu.Unlock()

	go l.keepAliveCtxCloser(ctx, id, ka.donec)
	// 使用once只在第一次调用
	l.firstKeepAliveOnce.Do(func() {
		// 500毫秒一次，不断的发送保持活动请求
		go l.recvKeepAliveLoop()
		// 删除等待太久没反馈的租约
		go l.deadlineLoop()
	})

	return ch, nil
}

// deadlineLoop获取在租约TTL中没有收到响应的任何保持活动的通道
func (l *lessor) deadlineLoop() {
	for {
		select {
		case <-time.After(time.Second):
			// donec 关闭，当 recvKeepAliveLoop 停止时设置 loopErr
		case <-l.donec:
			return
		}
		now := time.Now()
		l.mu.Lock()
		for id, ka := range l.keepAlives {
			if ka.deadline.Before(now) {
				// 等待响应太久；租约可能已过期
				ka.close()
				delete(l.keepAlives, id)
			}
		}
		l.mu.Unlock()
	}
}

func (l *lessor) recvKeepAliveLoop() (gerr error) {
	defer func() {
		l.mu.Lock()
		close(l.donec)
		l.loopErr = gerr
		for _, ka := range l.keepAlives {
			ka.close()
		}
		l.keepAlives = make(map[LeaseID]*keepAlive)
		l.mu.Unlock()
	}()

	for {
		// resetRecv 打开一个新的lease stream并开始发送保持活动请求。
		stream, err := l.resetRecv()
		if err != nil {
			if canceledByCaller(l.stopCtx, err) {
				return err
			}
		} else {
			for {
				// 接收lease stream的返回返回
				resp, err := stream.Recv()
				if err != nil {
					if canceledByCaller(l.stopCtx, err) {
						return err
					}

					if toErr(l.stopCtx, err) == rpctypes.ErrNoLeader {
						l.closeRequireLeader()
					}
					break
				}
				// 根据LeaseKeepAliveResponse更新租约
				// 如果租约过期删除所有alive channels
				l.recvKeepAlive(resp)
			}
		}

		select {
		case <-time.After(retryConnWait):
			continue
		case <-l.stopCtx.Done():
			return l.stopCtx.Err()
		}
	}
}

// resetRecv 打开一个新的lease stream并开始发送保持活动请求。
func (l *lessor) resetRecv() (pb.Lease_LeaseKeepAliveClient, error) {
	sctx, cancel := context.WithCancel(l.stopCtx)
	// 建立服务端和客户端连接的lease stream
	stream, err := l.remote.LeaseKeepAlive(sctx, l.callOpts...)
	if err != nil {
		cancel()
		return nil, err
	}

	l.mu.Lock()
	defer l.mu.Unlock()
	if l.stream != nil && l.streamCancel != nil {
		l.streamCancel()
	}

	l.streamCancel = cancel
	l.stream = stream

	go l.sendKeepAliveLoop(stream)
	return stream, nil
}

// sendKeepAliveLoop 在给定流的生命周期内发送保持活动请求
func (l *lessor) sendKeepAliveLoop(stream pb.Lease_LeaseKeepAliveClient) {
	for {
		var tosend []LeaseID

		now := time.Now()
		l.mu.Lock()
		for id, ka := range l.keepAlives {
			if ka.nextKeepAlive.Before(now) {
				tosend = append(tosend, id)
			}
		}
		l.mu.Unlock()

		for _, id := range tosend {
			r := &pb.LeaseKeepAliveRequest{ID: int64(id)}
			if err := stream.Send(r); err != nil {
				// TODO do something with this error?
				return
			}
		}

		select {
		// 每500毫秒执行一次
		case <-time.After(500 * time.Millisecond):
		case <-stream.Context().Done():
			return
		case <-l.donec:
			return
		case <-l.stopCtx.Done():
			return
		}
	}
}

// 撤销给定的租约，所有附加到租约的key将过期并被删除  
func (l *lessor) Revoke(ctx context.Context, id LeaseID) (*LeaseRevokeResponse, error) {
	r := &pb.LeaseRevokeRequest{ID: int64(id)}
	resp, err := l.remote.LeaseRevoke(ctx, r, l.callOpts...)
	if err == nil {
		return (*LeaseRevokeResponse)(resp), nil
	}
	return nil, toErr(ctx, err)
}
```

总结：  

1、每次注册一个服务的分配一个租约；  

2、KeepAlive通过从客户端到服务器端的流化的`keep alive`请求和从服务器端到客户端的流化的`keep alive`应答来维持租约；  

3、KeepAlive会500毫秒进行一次lease stream的发送；  

4、然后接收到KeepAlive发送信息回执，处理更新租约，服务处于活动状态；  

5、如果在租约TTL中没有收到响应的任何保持活动的请求，删除租约；  

6、Revoke撤销一个租约，所有附加到租约的key将过期并被删除。  

### 服务发现  

我们只需实现grpc在resolver中提供了Builder和Resolver接口，就能完成gRPC客户端的服务发现和负载均衡  

```go
// 创建一个resolver用于监视名称解析更新
type Builder interface {
	Build(target Target, cc ClientConn, opts BuildOption) (Resolver, error)
	Scheme() string
}
```

- Build方法：为给定目标创建一个新的resolver，当调用grpc.Dial()时执行；  

- Scheme方法：返回此resolver支持的方案,可参考[Scheme定义](https://github.com/grpc/grpc/blob/master/doc/naming.md)  

```go
// 监视指定目标的更新，包括地址更新和服务配置更新
type Resolver interface {
	ResolveNow(ResolveNowOption)
	Close()
}
```

- ResolveNow方法：被 gRPC 调用，以尝试再次解析目标名称。只用于提示，可忽略该方法;  

- Close方法：关闭resolver。  

接下来看下具体的实现  

```go
package discovery

import (
	"context"
	"time"

	"go.uber.org/zap"

	"go.etcd.io/etcd/api/v3/mvccpb"
	clientv3 "go.etcd.io/etcd/client/v3"
	"google.golang.org/grpc/resolver"
)

const (
	schema = "etcd"
)

// Resolver for grpc client
type Resolver struct {
	schema      string
	EtcdAddrs   []string
	DialTimeout int

	closeCh      chan struct{}
	watchCh      clientv3.WatchChan
	cli          *clientv3.Client
	keyPrifix    string
	srvAddrsList []resolver.Address

	cc     resolver.ClientConn
	logger *zap.Logger
}

// NewResolver create a new resolver.Builder base on etcd
func NewResolver(etcdAddrs []string, logger *zap.Logger) *Resolver {
	return &Resolver{
		schema:      schema,
		EtcdAddrs:   etcdAddrs,
		DialTimeout: 3,
		logger:      logger,
	}
}

// Scheme returns the scheme supported by this resolver.
func (r *Resolver) Scheme() string {
	return r.schema
}

// Build creates a new resolver.Resolver for the given target
func (r *Resolver) Build(target resolver.Target, cc resolver.ClientConn, opts resolver.BuildOptions) (resolver.Resolver, error) {
	r.cc = cc

	r.keyPrifix = BuildPrefix(Server{Name: target.Endpoint, Version: target.Authority})
	if _, err := r.start(); err != nil {
		return nil, err
	}
	return r, nil
}

// ResolveNow resolver.Resolver interface
func (r *Resolver) ResolveNow(o resolver.ResolveNowOptions) {}

// Close resolver.Resolver interface
func (r *Resolver) Close() {
	r.closeCh <- struct{}{}
}

// start
func (r *Resolver) start() (chan<- struct{}, error) {
	var err error
	r.cli, err = clientv3.New(clientv3.Config{
		Endpoints:   r.EtcdAddrs,
		DialTimeout: time.Duration(r.DialTimeout) * time.Second,
	})
	if err != nil {
		return nil, err
	}
	resolver.Register(r)

	r.closeCh = make(chan struct{})

	if err = r.sync(); err != nil {
		return nil, err
	}

	go r.watch()

	return r.closeCh, nil
}

// watch update events
func (r *Resolver) watch() {
	ticker := time.NewTicker(time.Minute)
	r.watchCh = r.cli.Watch(context.Background(), r.keyPrifix, clientv3.WithPrefix())

	for {
		select {
		case <-r.closeCh:
			return
		case res, ok := <-r.watchCh:
			if ok {
				r.update(res.Events)
			}
		case <-ticker.C:
			if err := r.sync(); err != nil {
				r.logger.Error("sync failed", zap.Error(err))
			}
		}
	}
}

// update
func (r *Resolver) update(events []*clientv3.Event) {
	for _, ev := range events {
		var info Server
		var err error

		switch ev.Type {
		case mvccpb.PUT:
			info, err = ParseValue(ev.Kv.Value)
			if err != nil {
				continue
			}
			addr := resolver.Address{Addr: info.Addr, Metadata: info.Weight}
			if !Exist(r.srvAddrsList, addr) {
				r.srvAddrsList = append(r.srvAddrsList, addr)
				r.cc.UpdateState(resolver.State{Addresses: r.srvAddrsList})
			}
		case mvccpb.DELETE:
			info, err = SplitPath(string(ev.Kv.Key))
			if err != nil {
				continue
			}
			addr := resolver.Address{Addr: info.Addr}
			if s, ok := Remove(r.srvAddrsList, addr); ok {
				r.srvAddrsList = s
				r.cc.UpdateState(resolver.State{Addresses: r.srvAddrsList})
			}
		}
	}
}

// sync 同步获取所有地址信息
func (r *Resolver) sync() error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	res, err := r.cli.Get(ctx, r.keyPrifix, clientv3.WithPrefix())
	if err != nil {
		return err
	}
	r.srvAddrsList = []resolver.Address{}

	for _, v := range res.Kvs {
		info, err := ParseValue(v.Value)
		if err != nil {
			continue
		}
		addr := resolver.Address{Addr: info.Addr, Metadata: info.Weight}
		r.srvAddrsList = append(r.srvAddrsList, addr)
	}
	r.cc.UpdateState(resolver.State{Addresses: r.srvAddrsList})
	return nil
}
```

总结：  

1、watch会监听前缀的信息变更，有变更的通知，及时更新srvAddrsList的地址信息；  

2、sync会定时的同步etcd中的可用的服务地址到srvAddrsList中；  

3、使用UpdateState更新ClientConn的Addresses。   

这里使用gRPC内置的负载均衡策略`round_robin`，根据负载均衡地址，以轮询的方式进行调用服务，来测试下服务的发现和简单的服务负载  

```go
package discovery

import (
	"context"
	"fmt"
	"log"
	"net"
	"testing"
	"time"

	"go.uber.org/zap"
	"google.golang.org/grpc/balancer/roundrobin"
	"google.golang.org/grpc/resolver"

	"etcd-learning/discovery/helloworld"

	"google.golang.org/grpc"
)

var etcdAddrs = []string{"127.0.0.1:2379"}

func TestResolver(t *testing.T) {
	r := NewResolver(etcdAddrs, zap.NewNop())
	resolver.Register(r)

	// etcd中注册5个服务
	go newServer(t, ":1001", "1.0.0", 1)
	go newServer(t, ":1002", "1.0.0", 1)
	go newServer(t, ":1003", "1.0.0", 1)
	go newServer(t, ":1004", "1.0.0", 1)
	go newServer(t, ":1006", "1.0.0", 10)

	conn, err := grpc.Dial("etcd:///hello", grpc.WithInsecure(), grpc.WithBalancerName(roundrobin.Name))
	if err != nil {
		t.Fatalf("failed to dial %v", err)
	}
	defer conn.Close()

	c := helloworld.NewGreeterClient(conn)

	// 进行十次数据请求
	for i := 0; i < 10; i++ {
		resp, err := c.SayHello(context.Background(), &helloworld.HelloRequest{Name: "abc"})
		if err != nil {
			t.Fatalf("say hello failed %v", err)
		}
		log.Println(resp.Message)
		time.Sleep(100 * time.Millisecond)
	}

	time.Sleep(10 * time.Second)
}

type server struct {
	Port string
}

// SayHello implements helloworld.GreeterServer
func (s *server) SayHello(ctx context.Context, in *helloworld.HelloRequest) (*helloworld.HelloReply, error) {
	return &helloworld.HelloReply{Message: fmt.Sprintf("Hello From %s", s.Port)}, nil
}

func newServer(t *testing.T, port string, version string, weight int64) {
	register := NewRegister(etcdAddrs, zap.NewNop())
	defer register.Stop()

	listen, err := net.Listen("tcp", port)
	if err != nil {
		log.Fatalf("failed to listen %v", err)
	}

	s := grpc.NewServer()
	helloworld.RegisterGreeterServer(s, &server{Port: port})

	info := Server{
		Name:    "hello",
		Addr:    fmt.Sprintf("127.0.0.1%s", port),
		Version: version,
		Weight:  weight,
	}

	register.Register(info, 10)

	if err := s.Serve(listen); err != nil {
		log.Fatalf("failed to server %v", err)
	}
}
```

这里注册了5个服务，端口号是1001到1006，循环调用10次   

```go
=== RUN   TestResolver
2021/07/24 22:44:52 Hello From :1001
2021/07/24 22:44:52 Hello From :1006
2021/07/24 22:44:53 Hello From :1001
2021/07/24 22:44:53 Hello From :1002
2021/07/24 22:44:53 Hello From :1003
2021/07/24 22:44:53 Hello From :1004
2021/07/24 22:44:53 Hello From :1006
2021/07/24 22:44:53 Hello From :1001
2021/07/24 22:44:53 Hello From :1002
2021/07/24 22:44:53 Hello From :1003
```

发现每次的请求会发送到不同的服务中   


### 负载均衡

#### 集中式LB（Proxy Model）  

<img src="/img/grpc_balance_1.png" alt="grpc" align=center/>

在服务消费者和服务提供者之间有一个独立的LB，通常是专门的硬件设备如 F5，或者基于软件如`LVS`，`HAproxy`等实现。LB上有所有服务的地址映射表，通常由运维配置注册，当服务消费方调用某个目标服务时，它向LB发起请求，由LB以某种策略，比如轮询`（Round-Robin）`做负载均衡后将请求转发到目标服务。LB一般具备健康检查能力，能自动摘除不健康的服务实例。  

该方案主要问题：  

1、单点问题，所有服务调用流量都经过LB，当服务数量和调用量大的时候，LB容易成为瓶颈，且一旦LB发生故障影响整个系统；  

2、服务消费方、提供方之间增加了一级，有一定性能开销。  

#### 进程内LB（Balancing-aware Client）   

<img src="/img/grpc_balance_2.png" alt="grpc" align=center/>

针对第一个方案的不足，此方案将LB的功能集成到服务消费方进程里，也被称为软负载或者客户端负载方案。服务提供方启动时，首先将服务地址注册到服务注册表，同时定期报心跳到服务注册表以表明服务的存活状态，相当于健康检查，服务消费方要访问某个服务时，它通过内置的LB组件向服务注册表查询，同时缓存并定期刷新目标服务地址列表，然后以某种负载均衡策略选择一个目标服务地址，最后向目标服务发起请求。LB和服务发现能力被分散到每一个服务消费者的进程内部，同时服务消费方和服务提供方之间是直接调用，没有额外开销，性能比较好。  

该方案主要问题：  

1、开发成本，该方案将服务调用方集成到客户端的进程里头，如果有多种不同的语言栈，就要配合开发多种不同的客户端，有一定的研发和维护成本；  

2、另外生产环境中，后续如果要对客户库进行升级，势必要求服务调用方修改代码并重新发布，升级较复杂。  

#### 独立 LB 进程（External Load Balancing Service）  

<img src="/img/grpc_balance_3.png" alt="grpc" align=center/>

该方案是针对第二种方案的不足而提出的一种折中方案，原理和第二种方案基本类似。  

不同之处是将LB和服务发现功能从进程内移出来，变成主机上的一个独立进程。主机上的一个或者多个服务要访问目标服务时，他们都通过同一主机上的独立LB进程做服务发现和负载均衡。该方案也是一种分布式方案没有单点问题，一个LB进程挂了只影响该主机上的服务调用方，服务调用方和LB之间是进程内调用性能好，同时该方案还简化了服务调用方，不需要为不同语言开发客户库，LB的升级不需要服务调用方改代码。  

该方案主要问题：部署较复杂，环节多，出错调试排查问题不方便。  

上面通过etcd实现服务发现，使用的及时第二种 进程内LB（Balancing-aware Client）。   

### 参考  

【Load Balancing in gRPC】https://github.com/grpc/grpc/blob/master/doc/load-balancing.md  
【文中的代码示例】https://github.com/boilingfrog/etcd-learning/tree/main/discovery    