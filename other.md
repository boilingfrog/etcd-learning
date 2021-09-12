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





