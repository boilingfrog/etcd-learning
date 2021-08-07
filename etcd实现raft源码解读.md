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

本文次阅读的etcd代码版本`v3.5.0`  

Etcd将raft协议实现为一个library，然后本身作为一个应用使用它。这个库仅仅实现了对应的raft算法，对于网络传输，磁盘存储，raft库没有做具体的实现，需要用户自己去实现。   

### raft实现

先来看几个源码中定义的一些变量概念  

- Node: 对etcd-raft模块具体实现的一层封装，方便上层模块使用etcd-raft模块；  

- 上层模块: etcd-raft的调用者，上层模块通过Node提供的API与底层的etcd-raft模块进行交互；  

- Cluster: 表示一个集群,其中记录了该集群的基础信息；  

- Member: 组层Cluster的元素之一，其中封装了一个节点的基本信息；  

- Peer: 集群中某个节点对集群中另一个节点的称呼；  

- Entry记录: 节点之间的传递是通过message进行的，每条消息中可以携带多条Entry记录,每条Entry对应一条一个独立的操作  

```go
type Entry struct {
	// Term：表示该Entry所在的任期。
	Term  uint64    `protobuf:"varint,2,opt,name=Term" json:"Term"`
	// Index:当前这个entry在整个raft日志中的位置索引,有了Term和Index之后，一个`log entry`就能被唯一标识。  
	Index uint64    `protobuf:"varint,3,opt,name=Index" json:"Index"`
	// 当前entry的类型
	// 目前etcd支持两种类型：EntryNormal和EntryConfChange 
	// EntryNormaln表示普通的数据操作
	// EntryConfChange表示集群的变更操作
	Type  EntryType `protobuf:"varint,1,opt,name=Type,enum=raftpb.EntryType" json:"Type"`
	// 具体操作使用的数据
	Data  []byte    `protobuf:"bytes,4,opt,name=Data" json:"Data,omitempty"`
}
```

- Message: 是所有消息的抽象，包括各种消息所需要的字段，raft集群中各个节点之前的通讯都是通过这个message进行的。  

```go
type Message struct {
	// 该字段定义了不同的消息类型，etcd-raft就是通过不同的消息类型来进行处理的，etcd中一共定义了19种类型
	Type MessageType `protobuf:"varint,1,opt,name=type,enum=raftpb.MessageType" json:"type"`
	// 消息的目标节点 ID，在急群中每个节点都有一个唯一的id作为标识
	To   uint64      `protobuf:"varint,2,opt,name=to" json:"to"`
	// 发送消息的节点ID
	From uint64      `protobuf:"varint,3,opt,name=from" json:"from"`
	// 整个消息发出去时，所处的任期
	Term uint64      `protobuf:"varint,4,opt,name=term" json:"term"`
	// 该消息携带的第一条Entry记录的的Term值
	LogTerm    uint64   `protobuf:"varint,5,opt,name=logTerm" json:"logTerm"`
	// 索引值，该索引值和消息的类型有关,不同的消息类型代表的含义不同
	Index      uint64   `protobuf:"varint,6,opt,name=index" json:"index"`
	// 需要存储的日志信息
	Entries    []Entry  `protobuf:"bytes,7,rep,name=entries" json:"entries"`
	// 已经提交的日志的索引值，用来向别人同步日志的提交信息。
	Commit     uint64   `protobuf:"varint,8,opt,name=commit" json:"commit"`
	// 在传输快照时，该字段保存了快照数据
	Snapshot   Snapshot `protobuf:"bytes,9,opt,name=snapshot" json:"snapshot"`
	// 主要用于响应类型的消息，表示是否拒绝收到的消息。  
	Reject     bool     `protobuf:"varint,10,opt,name=reject" json:"reject"`
	// Follower 节点拒绝 eader 节点的消息之后，会在该字段记录 一个Entry索引值供Leader节点。
	RejectHint uint64   `protobuf:"varint,11,opt,name=rejectHint" json:"rejectHint"`
	// 携带的一些上下文的信息
	Context    []byte   `protobuf:"bytes,12,opt,name=context" json:"context,omitempty"`
}
```

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
【raftexample 源码解读】https://zhuanlan.zhihu.com/p/91314329  
【etcd技术内幕】  












