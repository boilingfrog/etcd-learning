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

来看下Lease中的几个主要函数  

```go
// Lessor owns leases. It can grant, revoke, renew and modify leases for lessee.
type Lessor interface {
	// SetRangeDeleter lets the lessor create TxnDeletes to the store.
	// Lessor deletes the items in the revoked or expired lease by creating
	// new TxnDeletes.
	SetRangeDeleter(rd RangeDeleter)

	SetCheckpointer(cp Checkpointer)

	// Grant grants a lease that expires at least after TTL seconds.
	Grant(id LeaseID, ttl int64) (*Lease, error)
	// Revoke revokes a lease with given ID. The item attached to the
	// given lease will be removed. If the ID does not exist, an error
	// will be returned.
	Revoke(id LeaseID) error

	// Checkpoint applies the remainingTTL of a lease. The remainingTTL is used in Promote to set
	// the expiry of leases to less than the full TTL when possible.
	Checkpoint(id LeaseID, remainingTTL int64) error

	// Attach attaches given leaseItem to the lease with given LeaseID.
	// If the lease does not exist, an error will be returned.
	Attach(id LeaseID, items []LeaseItem) error

	// GetLease returns LeaseID for given item.
	// If no lease found, NoLease value will be returned.
	GetLease(item LeaseItem) LeaseID

	// Detach detaches given leaseItem from the lease with given LeaseID.
	// If the lease does not exist, an error will be returned.
	Detach(id LeaseID, items []LeaseItem) error

	// Promote promotes the lessor to be the primary lessor. Primary lessor manages
	// the expiration and renew of leases.
	// Newly promoted lessor renew the TTL of all lease to extend + previous TTL.
	Promote(extend time.Duration)

	// Demote demotes the lessor from being the primary lessor.
	Demote()

	// Renew renews a lease with given ID. It returns the renewed TTL. If the ID does not exist,
	// an error will be returned.
	Renew(id LeaseID) (int64, error)

	// Lookup gives the lease at a given lease id, if any
	Lookup(id LeaseID) *Lease

	// Leases lists all leases.
	Leases() []*Lease

	// ExpiredLeasesC returns a chan that is used to receive expired leases.
	ExpiredLeasesC() <-chan []*Lease

	// Recover recovers the lessor state from the given backend and RangeDeleter.
	Recover(b backend.Backend, rd RangeDeleter)

	// Stop stops the lessor for managing leases. The behavior of calling Stop multiple
	// times is undefined.
	Stop()
}
```




### 参考  

【Load Balancing in gRPC】https://github.com/grpc/grpc/blob/master/doc/load-balancing.md  
【文中的代码示例】https://github.com/boilingfrog/etcd-learning/tree/main/discovery    
【06 | 租约：如何检测你的客户端存活？】https://time.geekbang.org/column/article/339337  