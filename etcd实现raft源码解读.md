<!-- START doctoc generated TOC please keep comment here to allow auto update -->
<!-- DON'T EDIT THIS SECTION, INSTEAD RE-RUN doctoc TO UPDATE -->


- [etcd中raft实现源码解读](#etcd%E4%B8%ADraft%E5%AE%9E%E7%8E%B0%E6%BA%90%E7%A0%81%E8%A7%A3%E8%AF%BB)
  - [前言](#%E5%89%8D%E8%A8%80)
  - [raft实现](#raft%E5%AE%9E%E7%8E%B0)
  - [领导者选举](#%E9%A2%86%E5%AF%BC%E8%80%85%E9%80%89%E4%B8%BE)
    - [领导者选举](#%E9%A2%86%E5%AF%BC%E8%80%85%E9%80%89%E4%B8%BE-1)
  - [参考](#%E5%8F%82%E8%80%83)

<!-- END doctoc generated TOC please keep comment here to allow auto update -->

## etcd中raft实现源码解读

### 前言

关于raft的原来可参考[etcd学习(5)-etcd的Raft一致性算法原理](https://www.cnblogs.com/ricklz/p/15094389.html)  

Etcd将raft协议实现为一个library，然后本身作为一个应用使用它。这个库仅仅实现了对应的raft算法，对于网络传输，磁盘存储，raft库没有做具体的实现，需要用户自己去实现。   

### raft实现

### 领导者选举

对于node来讲，刚被出初始化的时候就是follower状态，当集群中的节点初次启动时会通过`StartNode()`函数启动创建对应的node实例和底层的raft实例。在`StartNode()`方法中，主要是根据传入的config配置创建raft实例并初始raft负使用的相关组件。   

```go
// etcd/raft/node.go
// Peer封装了节点的ID, peers记录了当前集群中全部节点的ID
func StartNode(c *Config, peers []Peer) Node {
	if len(peers) == 0 {
		panic("no peers given; use RestartNode instead")
	}
	// 根据config信息初始化RawNode
	// 同时也会初始化一个raft
	rn, err := NewRawNode(c)
	if err != nil {
		panic(err)
	}
	// 第一次使用初始化RawNode
	err = rn.Bootstrap(peers)
	if err != nil {
		c.Logger.Warningf("error occurred during starting a new node: %v", err)
	}
	// 初始化node实例
	n := newNode(rn)

	go n.run()
	return &n
}
```

重点来看下run 

```go
func (n *node) run() {
	...

	for {
		...
		select {
		case pm := <-propc:
			...
			r.Step(m)
		case m := <-n.recvc:
			...
			r.Step(m)
		case cc := <-n.confc:
			...
		case <-n.tickc:
			n.rn.Tick()
		case readyc <- rd:
			n.rn.acceptReady(rd)
			advancec = n.advancec
		case <-advancec:
			n.rn.Advance(rd)
			rd = Ready{}
			advancec = nil
		case c := <-n.status:
			c <- getStatus(r)
		case <-n.stop:
			close(n.done)
			return
		}
	}
}
```

总结：  

主要是通过`for-select-channel`监听channel信息，来处理不同的请求  

来看下几个主要的channel信息  

propc和recvc中拿到的是从上层应用传进来的消息，这个消息会被交给raft层的Step函数处理。  

```go
func (r *raft) Step(m pb.Message) error {
	//...
	switch m.Type {
	case pb.MsgHup:
	//...
	case pb.MsgVote, pb.MsgPreVote:
	//...
	default:
		r.step(r, m)
	}
}
```

Step是etcd-raft模块负责各类信息的入口  

### 定时器与心跳



#### 领导者选举

### 参考

【高可用分布式存储 etcd 的实现原理】https://draveness.me/etcd-introduction/  
【Raft 在 etcd 中的实现】https://blog.betacat.io/post/raft-implementation-in-etcd/  
【etcd Raft库解析】https://www.codedump.info/post/20180922-etcd-raft/  
【etcd raft 设计与实现《一》】https://zhuanlan.zhihu.com/p/51063866    










