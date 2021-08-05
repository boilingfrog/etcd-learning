## etcd中raft实现源码解读

### 前言

关于raft的原来可参考[etcd学习(5)-etcd的Raft一致性算法原理](https://www.cnblogs.com/ricklz/p/15094389.html)  

Etcd将raft协议实现为一个library，然后本身作为一个应用使用它。这个库仅仅实现了对应的raft算法，对于网络传输，磁盘存储，raft库没有做具体的实现，需要用户自己去实现。   

这里放一个整体上的
### 实现

首先对新加入的node进行初始化  

```go
// etcd/raft/node.go
func StartNode(c *Config, peers []Peer) Node {
	if len(peers) == 0 {
		panic("no peers given; use RestartNode instead")
	}
	rn, err := NewRawNode(c)
	if err != nil {
		panic(err)
	}
	err = rn.Bootstrap(peers)
	if err != nil {
		c.Logger.Warningf("error occurred during starting a new node: %v", err)
	}

	n := newNode(rn)

	go n.run()
	return &n
}
```


#### 领导者选举

### 参考

【高可用分布式存储 etcd 的实现原理】https://draveness.me/etcd-introduction/  
【Raft 在 etcd 中的实现】https://blog.betacat.io/post/raft-implementation-in-etcd/  
【etcd Raft库解析】https://www.codedump.info/post/20180922-etcd-raft/  
【etcd raft 设计与实现《一》】https://zhuanlan.zhihu.com/p/51063866    










