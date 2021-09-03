<!-- START doctoc generated TOC please keep comment here to allow auto update -->
<!-- DON'T EDIT THIS SECTION, INSTEAD RE-RUN doctoc TO UPDATE -->
**Table of Contents**  *generated with [DocToc](https://github.com/thlorenz/doctoc)*

- [etcd中的Lease](#etcd%E4%B8%AD%E7%9A%84lease)
  - [前言](#%E5%89%8D%E8%A8%80)
  - [Lease](#lease)
    - [Lease 整体架构](#lease-%E6%95%B4%E4%BD%93%E6%9E%B6%E6%9E%84)
  - [参考](#%E5%8F%82%E8%80%83)

<!-- END doctoc generated TOC please keep comment here to allow auto update -->

## etcd中的Lease

### 前言

之前我们了解过[grpc使用etcd做服务发现](https://www.cnblogs.com/ricklz/p/15059497.html)  

之前的服务发现我们使用了 Lease，每次注册一个服务分配一个租约，通过 Lease 自动上报机模式，实现了一种活性检测机制，保证了故障机器的及时剔除。这次我们来想写的学习 Lease 租约的实现。    

### Lease

#### Lease 整体架构

这里放一个来自【etcd实战课程】的一张图片  

<img src="/img/etcd-lease.png" alt="grpc" align=center/>

来看下服务端中Lease中的几个主要函数  

```go
// etcd/server/lease/lessor.go
// Lessor owns leases. It can grant, revoke, renew and modify leases for lessee.
type Lessor interface {
    ...
    // Grant 表示创建一个 TTL 为你指定秒数的 Lease
    Grant(id LeaseID, ttl int64) (*Lease, error)
    // Revoke 撤销具有给定 ID 的租约
    Revoke(id LeaseID) error
    
    // 将给定的租约附加到具有给定 LeaseID 的租约。
    Attach(id LeaseID, items []LeaseItem) error
    
    // Renew 使用给定的 ID 续订租约。它返回更新后的 TTL
    Renew(id LeaseID) (int64, error)
    ...
}
```

同时对于客户端 Lease 也提供了下面几个API    

```go
// etcd/client/v3/lease.go
type Lease interface {
    // Grant 表示创建一个 TTL 为你指定秒数的 Lease，Lessor 会将 Lease 信息持久化存储在 boltdb 中；
	Grant(ctx context.Context, ttl int64) (*LeaseGrantResponse, error)

	// 表示撤销 Lease 并删除其关联的数据；
	Revoke(ctx context.Context, id LeaseID) (*LeaseRevokeResponse, error)

	// 表示获取一个 Lease 的有效期、剩余时间；
	TimeToLive(ctx context.Context, id LeaseID, opts ...LeaseOption) (*LeaseTimeToLiveResponse, error)

	// Leases retrieves all leases.
	Leases(ctx context.Context) (*LeaseLeasesResponse, error)

    // 表示为 Lease 续期
	KeepAlive(ctx context.Context, id LeaseID) (<-chan *LeaseKeepAliveResponse, error)

    // 使用once只在第一次调用
	KeepAliveOnce(ctx context.Context, id LeaseID) (*LeaseKeepAliveResponse, error)

	// Close releases all resources Lease keeps for efficient communication
	// with the etcd server.
	Close() error
}
```






### 参考  

【Load Balancing in gRPC】https://github.com/grpc/grpc/blob/master/doc/load-balancing.md  
【文中的代码示例】https://github.com/boilingfrog/etcd-learning/tree/main/discovery    
【06 | 租约：如何检测你的客户端存活？】https://time.geekbang.org/column/article/339337  