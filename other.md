<!-- START doctoc generated TOC please keep comment here to allow auto update -->
<!-- DON'T EDIT THIS SECTION, INSTEAD RE-RUN doctoc TO UPDATE -->

- [etcd 中的点](#etcd-%E4%B8%AD%E7%9A%84%E7%82%B9)
  - [k8s 如何和 etcd 交互](#k8s-%E5%A6%82%E4%BD%95%E5%92%8C-etcd-%E4%BA%A4%E4%BA%92)
    - [Resource Version 与 etcd 版本号](#resource-version-%E4%B8%8E-etcd-%E7%89%88%E6%9C%AC%E5%8F%B7)
  - [如何支撑 k8s 中的上万节点](#%E5%A6%82%E4%BD%95%E6%94%AF%E6%92%91-k8s-%E4%B8%AD%E7%9A%84%E4%B8%8A%E4%B8%87%E8%8A%82%E7%82%B9)
    - [如何减少 expensive request](#%E5%A6%82%E4%BD%95%E5%87%8F%E5%B0%91-expensive-request)
  - [etcd 中分布式锁对比 redis 的安全性](#etcd-%E4%B8%AD%E5%88%86%E5%B8%83%E5%BC%8F%E9%94%81%E5%AF%B9%E6%AF%94-redis-%E7%9A%84%E5%AE%89%E5%85%A8%E6%80%A7)
    - [Redis 中分布式锁的缺点](#redis-%E4%B8%AD%E5%88%86%E5%B8%83%E5%BC%8F%E9%94%81%E7%9A%84%E7%BC%BA%E7%82%B9)

<!-- END doctoc generated TOC please keep comment here to allow auto update -->

## etcd 中的点

### k8s 如何和 etcd 交互

先来看下 k8s 中的架构  

<img src="/img/k8s-etcd.webp" alt="etcd" align=center/>  

etcd 主要是进行 Kubernetes 的元数据存储  

可以看到主要是 kube-apiserver 和 etcd 进行交互的   

kube-apiserver: 负责对外提供集群各类资源的增删改查及 Watch 接口，它是 Kubernetes 集群中各组件数据交互和通信的枢纽。kube-apiserver 在设计上可水平扩展，高可用 Kubernetes 集群中一般多副本部署。当收到一个创建 Pod 写请求时，它的基本流程是对请求进行认证、限速、授权、准入机制等检查后，写入到 etcd 即可。  

我们知道 k8s 中有 namespace 和标签，那么对于这种查询，是如何保持效率呢？  

- 按照资源名称查询  

按具体资源名称查询。它本质就是个 key-value 查询，只需要写入 etcd 的 key 名称与资源 key 一致即可。  

- 按 namespace 查询  

按照 namespace 查询，因为我们知道 etcd 支持范围查询，若 key 名称前缀包含 namespace、资源类型，查询的时候指定 namespace 和资源类型的组合的最小开始区间、最大结束区间即可。  

- 按照标签查询  

Kubernetes 中的查询方案是由 kube-apiserver 通过范围遍历 etcd 获取原始数据，然后基于用户指定标签，来筛选符合条件的资源返回给 client。  

当前缺点就是大量标签查询可能会导致 etcd 大流量等异常情况发生  

这里来看下 Kubernetes 集群中的 coredns 一系列资源在 etcd 中的存储格式  

```
/registry/clusterrolebindings/system:coredns
/registry/clusterroles/system:coredns
/registry/configmaps/kube-system/coredns
/registry/deployments/kube-system/coredns
/registry/events/kube-system/coredns-7fcc6d65dc-6njlg.1662c287aabf742b
/registry/events/kube-system/coredns-7fcc6d65dc-6njlg.1662c288232143ae
/registry/pods/kube-system/coredns-7fcc6d65dc-jvj26
/registry/pods/kube-system/coredns-7fcc6d65dc-mgvtb
/registry/pods/kube-system/coredns-7fcc6d65dc-whzq9
/registry/replicasets/kube-system/coredns-7fcc6d65dc
/registry/secrets/kube-system/coredns-token-hpqbt
/registry/serviceaccounts/kube-system/coredns
```

Kubernetes 资源在 etcd 中的存储格式由 prefix + "/" + 资源类型 + "/" + namespace + "/" + 具体资源名组成，基于 etcd 提供的范围查询能力，非常简单地支持了按具体资源名称查询和 namespace 查询。  

#### Resource Version 与 etcd 版本号

Kubernetes 集群中，它提供了什么概念来实现增量监听逻辑呢？  

Resource Version  

Resource Version 是 Kubernetes API 中非常重要的一个概念，顾名思义，它是一个 Kubernetes 资源的内部版本字符串，client 可通过它来判断资源是否发生了变化。同时，你可以在 Get、List、Watch 接口中，通过指定 Resource Version 值来满足你对数据一致性、高性能等诉求。  

下面从 Get 和 Watch 接口中的 Resource Version 参数值为例，来看下和 etcd 的关系  

在 Get 请求查询案例中，ResourceVersion 主要有以下这三种取值： 

- 指定 ResourceVersion 默认空字符串  

kube-apiserver 收到一个此类型的读请求后，它会向 etcd 发出共识读 / 线性读请求获取 etcd 集群最新的数据。  

- 指定 ResourceVersion="0"  

kube-apiserver 收到此类请求时，它可能会返回任意资源版本号的数据，但是优先返回较新版本。一般情况下它直接从 kube-apiserver 缓存中获取数据返回给 client，有可能读到过期的数据，适用于对数据一致性要求不高的场景。  

- 设置 ResourceVersion 为一个非 0 的字符串  

kube-apiserver 收到此类请求时，它会保证 Cache 中的最新 ResourceVersion 大于等于你传入的 ResourceVersion，然后从 Cache 中查找你请求的资源对象 key，返回数据给 client。基本原理是 kube-apiserver 为各个核心资源（如 Pod）维护了一个 Cache，通过 etcd 的 Watch 机制来实时更新 Cache。当你的 Get 请求中携带了非 0 的 ResourceVersion，它会等待缓存中最新 ResourceVersion 大于等于你 Get 请求中的 ResoureVersion，若满足条件则从 Cache 中查询数据，返回给 client。若不满足条件，它最多等待 3 秒，若超过 3 秒，Cache 中的最新 ResourceVersion 还小于 Get 请求中的 ResourceVersion，就会返回 ResourceVersionTooLarge 错误给 client。  

再看下 watch 请求查询案例中，ResourceVersion 主要有以下这三种取值：   

- 指定 ResourceVersion 默认空字符串  

一方面为了帮助 client 建立初始状态，它会将当前已存在的资源通过 Add 事件返回给 client。另一方面，它会从 etcd 当前版本号开始监听，后续新增写请求导致数据变化时可及时推送给 client。  

- 指定 ResourceVersion="0"  

它同样会帮助 client 建立初始状态，但是它会从任意版本号开始监听，这种场景可能导致集群返回成就的数据。  

- 指定 ResourceVersion 为一个非 0 的字符串

从精确的版本号开始监听数据，它只会返回大于等于精确版本号的变更事件。  

### 如何支撑 k8s 中的上万节点

大规模 Kubernetes 集群的外在表现是节点数成千上万，资源对象数量高达几十万。本质是更频繁地查询、写入更大的资源对象。  

当然大量的节点，对于写入和读取都会产生性能的影响  

#### 如何减少 expensive request

- 分页  

- 资源按 namespace 拆分 

- Informer 机制  

Informer 机制的 Reflector 封装了 Watch、List 操作，结合本地 Cache、Indexer，实现了控制器加载完初始状态数据后，接下来的其他操作都只需要从本地缓存读取，极大降低了 kube-apiserver 和 etcd 的压力。  

- Watch bookmark 机制  

Watch bookmark 机制通过新增一个 bookmark 类型的事件来实现的。kube-apiserver 会通过定时器将各类型资源最新的 Resource Version 推送给 kubelet 等 client，在 client 与 kube-apiserver 网络异常重连等场景，大大降低了 client 重建 Watch 的开销，减少了 relist expensive request。  

- 更高效的 Watch 恢复机制  

### etcd 中分布式锁对比 redis 的安全性

**分布式锁的几个核心要素**  

- 第一要素是互斥性、安全性。在同一时间内，不允许多个 client 同时获得锁。  

- 第二个要素就是活性  

在实现分布式锁的过程中要考虑到 client 可能会出现 crash 或者网络分区，你需要原子申请分布式锁及设置锁的自动过期时间，通过过期、超时等机制自动释放锁，避免出现死锁，导致业务中断。  

- 第三个要素是，高性能、高可用。加锁、释放锁的过程性能开销要尽量低，同时要保证高可用，确保业务不会出现中断。  

#### Redis 中分布式锁的缺点

比如主备切换  

比如主节点收到了请求，但是还没有同步到其他节点，然后主节点挂掉了，之后其他节点中有一个节点被选出来变成了主节点，但是刚刚的信息没有同步到，所以客户端的请求过来又会产生信息写入，造成互斥锁被获取到了两次。互斥性和安全性都被破坏掉了。  

主备切换、脑裂是 Redis 分布式锁的两个典型不安全的因素，本质原因是 Redis 为了满足高性能，采用了主备异步复制协议，同时也与负责主备切换的 Redis Sentinel 服务是否合理部署有关。  

对于这种情况，redis 中提供了 RedLock 算法，什么是 RedLock 算法呢？  

它是基于多个独立的 Redis Master 节点的一种实现（一般为 5）。client 依次向各个节点申请锁，若能从多数个节点中申请锁成功并满足一些条件限制，那么 client 就能获取锁成功。  

它通过独立的 N 个 Master 节点，避免了使用主备异步复制协议的缺陷，只要多数 Redis 节点正常就能正常工作，显著提升了分布式锁的安全性、可用性。  

#### 使用 etcd 作为分布式锁的优点  

**事务与锁的安全性** 

相比 Redis 基于主备异步复制导致锁的安全性问题，etcd 是基于 Raft 共识算法实现的，一个写请求需要经过集群多数节点确认。因此一旦分布式锁申请返回给 client 成功后，它一定是持久化到了集群多数节点上，不会出现 Redis 主备异步复制可能导致丢数据的问题，具备更高的安全性。  

**Lease 与锁的活性**

Lease 活动检测机制，client 需定期向 etcd 服务发送"特殊心跳"汇报健康状态，若你未正常发送心跳，并超过和 etcd 服务约定的最大存活时间后，就会被 etcd 服务移除此 Lease 和其关联的数据。  

**Watch 与锁的可用性**

Watch 提供了高效的数据监听能力。当其他 client 收到 Watch Delete 事件后，就可快速判断自己是否有资格获得锁，极大减少了锁的不可用时间。  

**etcd 自带的 concurrency 包**

etcd 社区提供了一个名为 concurrency 包帮助你更简单、正确地使用分布式锁、分布式选举。  

核心流程  

- 首先通过 concurrency.NewSession 方法创建 Session，本质是创建了一个 TTL 为 10 的 Lease。  

- 其次得到 session 对象后，通过 concurrency.NewMutex 创建了一个 mutex 对象，包含 Lease、key prefix 等信息。  

- 然后通过 mutex 对象的 Lock 方法尝试获取锁。  

- 最后使用结束，可通过 mutex 对象的 Unlock 方法释放锁。  

通过多个 concurrency 创建 prefix 相同，名称不一样的 key，哪个 key 的 revision 最小，最终就是它获得锁  

那未获得锁的 client 是如何等待的呢?  

答案是通过 Watch 机制各自监听 prefix 相同，revision 比自己小的 key，因为只有 revision 比自己小的 key 释放锁，我才能有机会，获得锁。  

