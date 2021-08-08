<!-- START doctoc generated TOC please keep comment here to allow auto update -->
<!-- DON'T EDIT THIS SECTION, INSTEAD RE-RUN doctoc TO UPDATE -->

- [etcd中raft实现源码解读](#etcd%E4%B8%ADraft%E5%AE%9E%E7%8E%B0%E6%BA%90%E7%A0%81%E8%A7%A3%E8%AF%BB)
  - [前言](#%E5%89%8D%E8%A8%80)
  - [raft实现](#raft%E5%AE%9E%E7%8E%B0)
  - [看下etcd中的raftexample](#%E7%9C%8B%E4%B8%8Betcd%E4%B8%AD%E7%9A%84raftexample)
    - [newRaftNode](#newraftnode)
    - [startRaft](#startraft)
  - [领导者选举](#%E9%A2%86%E5%AF%BC%E8%80%85%E9%80%89%E4%B8%BE)
  - [定时器与心跳](#%E5%AE%9A%E6%97%B6%E5%99%A8%E4%B8%8E%E5%BF%83%E8%B7%B3)
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

### 看下etcd中的raftexample

这里先看下etcd中提供的raftexample来简单连接下etcd中raft的使用  

这里放一张raftexample总体的架构图  

<img src="/img/etcd-raftExample.jpg" alt="etcd" align=center/>

raftexample 是一个`etcd raft library`的使用示例。它为Raft一致性算法的键值对集群存储提供了一个简单的`REST API`。  

来看下几个主要的函数实现  

#### newRaftNode

在该函数中主要完成了raftNode的初始化 。在该方法中会使用上层模块传入的配置信息(其中包括proposeC通道和confChangeC通道)来创建raftNode实例，同时会创建commitC通道和errorC通道返回给上层模块使用 。这样，上层模块就可以通过这几个通道与rafeNode实例进行 交互了。另外，newRaftNode()函数中还会启动一个独立的后台goroutine来完成回放WAL日志、 启动网络组件等初始化操作。 

```go
// 主要完成了raftNode的初始化
// 使用上层模块传入的配置信息来创建raftNode实例，同时创建commitC 通道和errorC通道返回给上层模块使用
// 上层的应用通过这几个channel就能和raftNode进行交互
func newRaftNode(id int, peers []string, join bool, getSnapshot func() ([]byte, error), proposeC <-chan string,
	confChangeC <-chan raftpb.ConfChange) (<-chan *commit, <-chan error, <-chan *snap.Snapshotter) {
	// channel，主要传输Entry记录
	// raftNode会将etcd-raft模块返回的待应用Entry记
	// 录（封装在 Ready实例中〉写入commitC通道，另一方面，kvstore会从commitC通
	// 道中读取这些待应用的 Entry 记录井保存其中的键值对信息。
	commitC := make(chan *commit)
	errorC := make(chan error)

	rc := &raftNode{
		proposeC:    proposeC,
		confChangeC: confChangeC,
		commitC:     commitC,
		errorC:      errorC,
		id:          id,
		peers:       peers,
		join:        join,
		// 初始化存放 WAL 日志和 Snapshot 文件的的目录
		waldir:      fmt.Sprintf("raftexample-%d", id),
		snapdir:     fmt.Sprintf("raftexample-%d-snap", id),
		getSnapshot: getSnapshot,
		snapCount:   defaultSnapshotCount,
		stopc:       make(chan struct{}),
		httpstopc:   make(chan struct{}),
		httpdonec:   make(chan struct{}),

		logger: zap.NewExample(),

		snapshotterReady: make(chan *snap.Snapshotter, 1),
		// rest of structure populated after WAL replay
	}
	// 启动一个goroutine,完成剩余的初始化工作
	go rc.startRaft()
	return commitC, errorC, rc.snapshotterReady
}
```

#### startRaft

1、创建 Snapshotter，并将该 Snapshotter 实例返回给上层模块；  

2、创建 WAL 实例，然后加载快照并回放 WAL 日志；  

3、创建 raft.Config 实例，其中包含了启动 etcd-raft 模块的所有配置；  

4、初始化底层 etcd-raft 模块，得到 node 实例；  

5、创建 Transport 实例，该实例负责集群中各个节点之间的网络通信，其具体实现在 raft-http 包中；  

6、建立与集群中其他节点的网络连接；  

7、启动网络组件，其中会监听当前节点与集群中其他节点之间的网络连接，并进行节点之间的消息读写；  

8、启动两个后台的 goroutine，它们主要工作是处理上层模块与底层 etcd-raft 模块的交互，但处理的具体内容不同，后面会详细介绍这两个 goroutine 的处理流程。  

```go
func (rc *raftNode) startRaft() {
	if !fileutil.Exist(rc.snapdir) {
		if err := os.Mkdir(rc.snapdir, 0750); err != nil {
			log.Fatalf("raftexample: cannot create dir for snapshot (%v)", err)
		}
	}
	rc.snapshotter = snap.New(zap.NewExample(), rc.snapdir)
	// 创建 WAL 实例，然后加载快照并回放 WAL 日志
	oldwal := wal.Exist(rc.waldir)

	// raftNode.replayWAL() 方法首先会读取快照数据，
	//在快照数据中记录了该快照包含的最后一条 Entry 记录的 Term 值 和 索引值。
	//然后根据 Term 值 和 索引值确定读取 WAL 日志文件的位置， 并进行日志记录的读取。
	rc.wal = rc.replayWAL()

	// signal replay has finished
	rc.snapshotterReady <- rc.snapshotter

	rpeers := make([]raft.Peer, len(rc.peers))
	for i := range rpeers {
		rpeers[i] = raft.Peer{ID: uint64(i + 1)}
	}
	// 创建 raft.Config 实例
	c := &raft.Config{
		ID: uint64(rc.id),
		// 选举超时
		ElectionTick: 10,
		// 心跳超时
		HeartbeatTick:             1,
		Storage:                   rc.raftStorage,
		MaxSizePerMsg:             1024 * 1024,
		MaxInflightMsgs:           256,
		MaxUncommittedEntriesSize: 1 << 30,
	}
	// 初始化底层的 etcd-raft 模块，这里会根据 WAL 日志的回放情况，
	// 判断当前节点是首次启动还是重新启动
	if oldwal || rc.join {
		rc.node = raft.RestartNode(c)
	} else {
		// 初次启动
		rc.node = raft.StartNode(c, rpeers)
	}
	// 创建 Transport 实例并启动，他负责 raft 节点之间的网络通信服务
	rc.transport = &rafthttp.Transport{
		Logger:      rc.logger,
		ID:          types.ID(rc.id),
		ClusterID:   0x1000,
		Raft:        rc,
		ServerStats: stats.NewServerStats("", ""),
		LeaderStats: stats.NewLeaderStats(zap.NewExample(), strconv.Itoa(rc.id)),
		ErrorC:      make(chan error),
	}
	// 启动网络服务相关组件
	rc.transport.Start()
	// 建立与集群中其他各个节点的连接
	for i := range rc.peers {
		if i+1 != rc.id {
			rc.transport.AddPeer(types.ID(i+1), []string{rc.peers[i]})
		}
	}
	// 启动一个goroutine，其中会监听当前节点与集群中其他节点之间的网络连接
	go rc.serveRaft()
	// 启动后台 goroutine 处理上层应用与底层 etcd-raft 模块的交互
	go rc.serveChannels()
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












