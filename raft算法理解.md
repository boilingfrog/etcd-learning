## ETCD的Raft一致性算法原理

### Raft原理了解

Raft 是一种为了管理复制日志的一致性算法。它提供了和 Paxos 算法相同的功能和性能，但是它的算法结构和 Paxos 不同，使得 Raft 算法更加容易理解并且更容易构建实际的系统。 

Raft是一种分布式一致性算法。它被设计得易于理解, 解决了即使在出现故障时也可以让多个服务器对共享状态达成一致的问题。共享状态通常是通过日志复制支持的数据结构。只要大多数服务器是正常运作的，系统就能全面运行。  

Raft的工作方式是在集群中选举一个领导者。领导者负责接受客户端请求并管理到其他服务器的日志复制。数据只在一个方向流动:从领导者到其他服务器。  

Raft将一致性问题分解为三个子问题:

- 领导者选举: 现有领导者失效时，需要选举新的领导者；  

- 日志复制: 领导者需要通过复制保持所有服务器的日志与自己的同步；  

- 安全性: 如果其中一个服务器在特定索引上提交了日志条目，那么其他服务器不能在该索引应用不同的日志条目。   






### 参考  

【一文搞懂Raft算法】https://www.cnblogs.com/xybaby/p/10124083.html    
【寻找一种易于理解的一致性算法（扩展版）】https://github.com/maemual/raft-zh_cn/blob/master/raft-zh_cn.md  
【raft演示动画】https://raft.github.io/raftscope/index.html    
【理解 raft 算法】https://sanyuesha.com/2019/04/18/raft/  
【理解Raft一致性算法—一篇学术论文总结】https://mp.weixin.qq.com/s/RkMeYyUck1WQPjNiGvahKQ  

