<!-- START doctoc generated TOC please keep comment here to allow auto update -->
<!-- DON'T EDIT THIS SECTION, INSTEAD RE-RUN doctoc TO UPDATE -->

- [线性一致性](#%E7%BA%BF%E6%80%A7%E4%B8%80%E8%87%B4%E6%80%A7)
  - [CAP](#cap)
  - [什么是CAP](#%E4%BB%80%E4%B9%88%E6%98%AFcap)
  - [CAP的权衡](#cap%E7%9A%84%E6%9D%83%E8%A1%A1)
    - [AP wihtout C](#ap-wihtout-c)
    - [CA without P](#ca-without-p)
    - [CP without A](#cp-without-a)
  - [线性一致性](#%E7%BA%BF%E6%80%A7%E4%B8%80%E8%87%B4%E6%80%A7-1)
  - [etcd中如何实现线性一致性](#etcd%E4%B8%AD%E5%A6%82%E4%BD%95%E5%AE%9E%E7%8E%B0%E7%BA%BF%E6%80%A7%E4%B8%80%E8%87%B4%E6%80%A7)
    - [线性一致性写](#%E7%BA%BF%E6%80%A7%E4%B8%80%E8%87%B4%E6%80%A7%E5%86%99)
    - [线性一致性读](#%E7%BA%BF%E6%80%A7%E4%B8%80%E8%87%B4%E6%80%A7%E8%AF%BB)
  - [参考](#%E5%8F%82%E8%80%83)

<!-- END doctoc generated TOC please keep comment here to allow auto update -->

## 线性一致性

### CAP

### 什么是CAP

在聊什么是线性一致性的时候，我们先来看看什么是CAP  

CAP理论：一个分布式系统最多只能同时满足一致性（Consistency）、可用性（Availability）和分区容错性（Partition tolerance）这三项中的两项。  

- 1、一致性（Consistency）

一致性表示所有客户端同时看到相同的数据，无论它们连接到哪个节点。 要做到这一点，每当数据写入一个节点时，就必须立即将其转发或复制到系统中的所有其他节点，然后写操作才被视为“成功”。  

- 2、可用性（Availability） 

可用性表示发出数据请求的任何客户端都会得到响应，即使一个或多个节点宕机。 可用性的另一种状态：分布式系统中的所有工作节点都返回任何请求的有效响应，而不会发生异常。   

- 3、分区容错性（Partition tolerance）  

分区是分布式系统中的通信中断 - 两个节点之间的丢失连接或连接临时延迟。 分区容错意味着集群必须持续工作，无论系统中的节点之间有任何数量的通信中断。  

### CAP的权衡

根据定理，分布式系统只能满足三项中的两项而不可能满足全部三项。  

#### AP wihtout C

允许分区下的高可用，就需要放弃一致性。一旦分区发生，节点之间可能会失去联系，为了高可用，每个节点只能用本地数据提供服务，而这样会导致全局数据的不一致性。  

#### CA without P  

如果不会出现分区，一直性和可用性是可以同时保证的。但是我们现在的系统基本上是都是分布式的，也就是我们的服务肯定是被多台机器所提供的，所以分区就难以避免。  

#### CP without A  

如果不要求A（可用），相当于每个请求都需要在Server之间强一致，而P（分区）会导致同步时间无限延长，如此CP也是可以保证的。   

### 线性一致性

线性一致性又叫做原子一致性，强一致性。线性一致性可以看做只有一个单核处理器，或者可以看做只有一个数据副本，并且所有操作都是原子的。在可线性化的分布式系统中，如果某个节点更新了数据，那么在其他节点如果都能读取到这个最新的数据。可以看见线性一致性和我们的CAP中的C是一致的。  

举个非线性一致性的例子，比如有个秒杀活动，你和你的朋友同时去抢购一样东西，有可能他那里的库存已经没了，但是在你手机上显示还有几件，这个就违反了线性一致性，哪怕过了一会你的手机也显示库存没有，也依然是违反了。  

### etcd中如何实现线性一致性

#### 线性一致性写

所有的写操作，都要经过leader节点，一旦leader被选举成功，就可以对客户端提供服务了。客户端提交每一条命令都会被按顺序记录到leader的日志中，每一条命令都包含term编号和顺序索引，然后向其他节点并行发送AppendEntries RPC用以复制命令(如果命令丢失会不断重发)，当复制成功也就是大多数节点成功复制后，leader就会提交命令，即执行该命令并且将执行结果返回客户端，raft保证已经提交的命令最终也会被其他节点成功执行。具体源码参见[日志同步](https://www.cnblogs.com/ricklz/p/15155095.html#%E6%97%A5%E5%BF%97%E5%90%8C%E6%AD%A5)      

因为日志是顺序记录的，并且有严格的确认机制，所以可以认为写是满足线性一致性的。   

由于在Raft算法中，写操作成功仅仅意味着日志达成了一致（已经落盘），而并不能确保当前状态机也已经apply了日志。状态机apply日志的行为在大多数Raft算法的实现中都是异步的，所以此时读取状态机并不能准确反应数据的状态，很可能会读到过期数据。  

如何实现读取的线性一致性，就需要引入ReadIndex了  

#### 线性一致性读  

ReadIndex算法：  

每次读操作的时候记录此时集群的`commited index`，当状态机的`apply index`大于或等于`commited index`时才读取数据并返回。由于此时状态机已经把读请求发起时的已提交日志进行了apply动作，所以此时状态机的状态就可以反应读请求发起时的状态，符合线性一致性读的要求。  

Leader执行ReadIndex大致的流程如下：

- 1、记录当前的commit index，称为ReadIndex；  

所有的请求都会交给leader，如果follower收到读请求，会将请求forward给leader

- 2、向 Follower 发起一次心跳，如果大多数节点回复了，那就能确定现在仍然是 Leader；  

确认当前leader的状态

- 3、等待状态机的apply index大于或等于commited index时才读取数据；    

`apply index`大于或等于`commited index`就能表示当前状态机已经把读请求发起时的已提交日志进行了apply动作，所以此时状态机的状态就可以反应读请求发起时的状态，满足一致性读；  

- 4、执行读请求，将结果返回给Client。  




            
### 参考

【CAP定理】https://zh.wikipedia.org/wiki/CAP%E5%AE%9A%E7%90%86  
【CAP定理】https://www.ibm.com/cn-zh/cloud/learn/cap-theorem  
【线性一致性：什么是线性一致性？】https://zhuanlan.zhihu.com/p/42239873    
【什么是数据一致性】https://github.com/javagrowing/JGrowing/blob/master/%E5%88%86%E5%B8%83%E5%BC%8F/%E8%B0%88%E8%B0%88%E6%95%B0%E6%8D%AE%E4%B8%80%E8%87%B4%E6%80%A7.md    
【etcd 中线性一致性读的具体实现】https://zhengyinyong.com/post/etcd-linearizable-read-implementation/  


