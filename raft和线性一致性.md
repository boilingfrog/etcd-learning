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

假设有两个节点处于分区的两侧，允许至少一个节点更新状态会导致数据不一致，即丧失了C性质。如果为了保证数据一致性，将分区一侧的节点设置为不可用，那么又丧失了A性质。除非两个节点可以互相通信，才能既保证C又保证A，但是不同分区之前的通信时间是不能保证的  

#### AP wihtout C

允许分区下的高可用，就需要放弃一致性。一旦分区发生，节点之间可能会失去联系，为了高可用，每个节点只能用本地数据提供服务，而这样会导致全局数据的不一致性。  

#### CA without P  

如果不会出现分区，一直性和可用性是可以同时保证的。但是我们现在的系统基本上是都是分布式的，也就是我们的服务肯定是被多台机器所提供的，所以分区就难以避免。  

#### CP without A  

如果不要求A（可用），相当于每个请求都需要在Server之间强一致，而P（分区）会导致同步时间无限延长，如此CP也是可以保证的。   

### 线性一致性

线性一致性又叫做原子一致性，强一致性。线性一致性可以看做只有一个单核处理器，或者可以看做只有一个数据副本，并且所有操作都是原子的。在可线性化的分布式系统中，如果某个节点更新了数据，那么在其他节点如果都能读取到这个最新的数据。可以看见线性一致性和我们的CAP中的C是一致的。  

举个非线性一致性的例子，比如有个秒杀活动，你和你的朋友同时去抢购一样东西，有可能他那里的库存已经没了，但是在你手机上显示还有几件，这个就违反了线性一致性，哪怕过了一会你的手机也显示库存没有，也依然是违反了。  




### 参考

【CAP定理】https://zh.wikipedia.org/wiki/CAP%E5%AE%9A%E7%90%86  
【CAP定理】https://www.ibm.com/cn-zh/cloud/learn/cap-theorem  
【线性一致性：什么是线性一致性？】https://zhuanlan.zhihu.com/p/42239873    
【什么是数据一致性】https://github.com/javagrowing/JGrowing/blob/master/%E5%88%86%E5%B8%83%E5%BC%8F/%E8%B0%88%E8%B0%88%E6%95%B0%E6%8D%AE%E4%B8%80%E8%87%B4%E6%80%A7.md    


