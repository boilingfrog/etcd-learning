<!-- START doctoc generated TOC please keep comment here to allow auto update -->
<!-- DON'T EDIT THIS SECTION, INSTEAD RE-RUN doctoc TO UPDATE -->


- [线性一致性](#%E7%BA%BF%E6%80%A7%E4%B8%80%E8%87%B4%E6%80%A7)
  - [CAP](#cap)
  - [什么是CAP](#%E4%BB%80%E4%B9%88%E6%98%AFcap)
  - [CAP的权衡](#cap%E7%9A%84%E6%9D%83%E8%A1%A1)
    - [AP wihtout C](#ap-wihtout-c)
    - [CA without P](#ca-without-p)
    - [CP without A](#cp-without-a)
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




### 参考

【CAP定理】https://zh.wikipedia.org/wiki/CAP%E5%AE%9A%E7%90%86  