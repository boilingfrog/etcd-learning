## etcd中的存储实现

### 前言

前面了关于etcd的raft相关的实现，这里来看下存储的相关实现  

通过看etcd的代码我们可以看到目前有两个大的版本，v2和v3。

### V3和V2版本的对比

etcd的v2版本有下面的一些问题  

**功能局限性**  

1、etcd v2 不支持范围查询和分页；  

2、etcd v2 不支持多 key 事务；  

**Watch 机制可靠性问题**  

etcd v2 是内存型、不支持保存 key 历史版本的数据库，只在内存中使用滑动窗口保存了最近的 1000 条变更事件，当 etcd server 写请求较多、网络波动时等场景，很容易出现事件丢失问题，进而又触发 client 数据全量拉取，产生大量 expensive request，甚至导致 etcd 雪崩。  

**性能瓶颈问题**

1、etcd v2早起使用的是 HTTP/1.x API。HTTP/1.x 协议没有压缩机制，大量的请求可能导致 etcd 出现 CPU 高负载、OOM、丢包等问题；  

2、etcd v2 client 会通过 HTTP 长连接轮询 Watch 事件，当 watcher 较多的时候，因 HTTP/1.x 不支持多路复用，会创建大量的连接，消耗 server 端过多的 socket 和内存资源；  

3、对于 key 中的 TTL过期时间，如果大量 key TTL 一样，也需要分别为每个 key 发起续期操作，当 key 较多的时候，这会显著增加集群负载、导致集群性能显著下降；  

`内存开销问题`

etcd v2 在内存维护了一颗树来保存所有节点 key 及 value。在数据量场景略大的场景，如配置项较多、存储了大量 Kubernetes Events， 它会导致较大的内存开销，同时 etcd 需要定时把全量内存树持久化到磁盘。这会消耗大量的 CPU 和磁盘 I/O 资源，对系统的稳定性造成一定影响。  

etcd v3 的出现就是为了解决以上稳定性、扩展性、性能问题  

1、在内存开销、Watch 事件可靠性、功能局限上，它通过引入 B-tree、boltdb 实现一个 MVCC 数据库，数据模型从层次型目录结构改成扁平的 key-value，提供稳定可靠的事件通知，实现了事务，支持多 key 原子更新，同时基于 boltdb 的持久化存储，显著降低了 etcd 的内存占用、避免了 etcd v2 定期生成快照时的昂贵的资源开销；    

2、etcd v3 使用了 gRPC API，使用 protobuf 定义消息，消息编解码性能相比 JSON 超过 2 倍以上，并通过 HTTP/2.0 多路复用机制，减少了大量 watcher 等场景下的连接数；   

3、使用 Lease 优化 TTL 机制，每个 Lease 具有一个 TTL，相同的 TTL 的 key 关联一个 Lease，Lease 过期的时候自动删除相关联的所有 key，不再需要为每个 key 单独续期；  

4、etcd v3 支持范围、分页查询，可避免大包等 expensive request。  