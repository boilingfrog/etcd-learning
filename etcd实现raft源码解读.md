<!-- START doctoc generated TOC please keep comment here to allow auto update -->
<!-- DON'T EDIT THIS SECTION, INSTEAD RE-RUN doctoc TO UPDATE -->

- [etcd中raft实现源码解读](#etcd%E4%B8%ADraft%E5%AE%9E%E7%8E%B0%E6%BA%90%E7%A0%81%E8%A7%A3%E8%AF%BB)
  - [前言](#%E5%89%8D%E8%A8%80)
  - [raft实现](#raft%E5%AE%9E%E7%8E%B0)
  - [看下etcd中的raftexample](#%E7%9C%8B%E4%B8%8Betcd%E4%B8%AD%E7%9A%84raftexample)
    - [newRaftNode](#newraftnode)
    - [startRaft](#startraft)
    - [serveChannels](#servechannels)
  - [领导者选举](#%E9%A2%86%E5%AF%BC%E8%80%85%E9%80%89%E4%B8%BE)
  - [启动并初始化node节点](#%E5%90%AF%E5%8A%A8%E5%B9%B6%E5%88%9D%E5%A7%8B%E5%8C%96node%E8%8A%82%E7%82%B9)
  - [发送心跳包](#%E5%8F%91%E9%80%81%E5%BF%83%E8%B7%B3%E5%8C%85)
    - [作为leader](#%E4%BD%9C%E4%B8%BAleader)
    - [作为follower](#%E4%BD%9C%E4%B8%BAfollower)
    - [作为candidate](#%E4%BD%9C%E4%B8%BAcandidate)
  - [leader选举](#leader%E9%80%89%E4%B8%BE)
    - [1、接收leader的心跳](#1%E6%8E%A5%E6%94%B6leader%E7%9A%84%E5%BF%83%E8%B7%B3)
    - [2、发起竞选](#2%E5%8F%91%E8%B5%B7%E7%AB%9E%E9%80%89)
    - [3、其他节点收到信息，进行投票](#3%E5%85%B6%E4%BB%96%E8%8A%82%E7%82%B9%E6%94%B6%E5%88%B0%E4%BF%A1%E6%81%AF%E8%BF%9B%E8%A1%8C%E6%8A%95%E7%A5%A8)
    - [4、candidate节点统计投票的结果](#4candidate%E8%8A%82%E7%82%B9%E7%BB%9F%E8%AE%A1%E6%8A%95%E7%A5%A8%E7%9A%84%E7%BB%93%E6%9E%9C)
  - [日志同步](#%E6%97%A5%E5%BF%97%E5%90%8C%E6%AD%A5)
    - [WAL日志](#wal%E6%97%A5%E5%BF%97)
    - [leader同步follower日志](#leader%E5%90%8C%E6%AD%A5follower%E6%97%A5%E5%BF%97)
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

- raftLog: Raft中日志同步的核心就是集群中leader如何同步日志到各个follower。日志的管理是在raftLog结构上完成的。  

```go
type raftLog struct {
	// 用于保存自从最后一次snapshot之后提交的数据
	storage Storage

	// 用于保存还没有持久化的数据和快照，这些数据最终都会保存到storage中
	unstable unstable

	// 当天提交的日志数据索引
	committed uint64
	// committed保存是写入持久化存储中的最高index，而applied保存的是传入状态机中的最高index
	// 即一条日志首先要提交成功（即committed），才能被applied到状态机中
	// 因此以下不等式一直成立：applied <= committed
	applied uint64

	logger Logger

	// 调用 nextEnts 时，返回的日志项集合的最大的大小
	// nextEnts 函数返回应用程序已经可以应用到状态机的日志项集合
	maxNextEntsSize uint64
}
```

### 看下etcd中的raftexample

这里先看下etcd中提供的raftexample来简单连接下etcd中raft的使用  

这里放一张raftexample总体的架构图  

<img src="/img/etcd-raftExample.jpg" alt="etcd" align=center/>

raftexample 是一个`etcd raft library`的使用示例。它为Raft一致性算法的键值对集群存储提供了一个简单的`REST API`。  

该包提供了goreman启动集群的方式，使用`goreman start`启动，可以很清楚的看到raft在启动过程中的选举过程，能够很好的帮助我们理解raft的选举过程  
  
<img src="/img/raftexample.jpg" alt="etcd" align=center/>

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

#### serveChannels

处理上层应用与底层etcd-raft模块的交互  

```go
// 会单独启动一个后台 goroutine来负责上层模块 传递给 etcd-ra企 模块的数据，
// 主要 处理前面介绍的 proposeC、 confChangeC 两个通道
func (rc *raftNode) serveChannels() {
	// 这里是获取快照数据和快照的元数据
	snap, err := rc.raftStorage.Snapshot()
	if err != nil {
		panic(err)
	}
	rc.confState = snap.Metadata.ConfState
	rc.snapshotIndex = snap.Metadata.Index
	rc.appliedIndex = snap.Metadata.Index

	defer rc.wal.Close()

	// 创建一个每隔 lOOms 触发一次的定时器，那么在逻辑上，lOOms 即是 etcd-raft 组件的最小时间单位 ，
	// 该定时器每触发一次，则逻辑时钟推进一次
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	// 单独启 动一个 goroutine 负责将 proposeC、 confChangeC 远远上接收到
	// 的数据传递给 etcd-raft 组件进行处理
	go func() {
		confChangeCount := uint64(0)

		for rc.proposeC != nil && rc.confChangeC != nil {
			select {
			case prop, ok := <-rc.proposeC:
				if !ok {
					// 发生异常将proposeC置空
					rc.proposeC = nil
				} else {
					// 阻塞直到消息被处理
					rc.node.Propose(context.TODO(), []byte(prop))
				}
				// 收到上层应用通过 confChangeC远远传递过来的数据
			case cc, ok := <-rc.confChangeC:
				if !ok {
					// 如果发生异常将confChangeC置空
					rc.confChangeC = nil
				} else {
					confChangeCount++
					cc.ID = confChangeCount
					rc.node.ProposeConfChange(context.TODO(), cc)
				}
			}
		}
		// 关闭 stopc 通道，触发 rafeNode.stop() 方法的调用
		close(rc.stopc)
	}()

	// 处理 etcd-raft 模块返回给上层模块的数据及其他相关的操作
	for {
		select {
		case <-ticker.C:
			// 上述 ticker 定时器触发一次
			rc.node.Tick()

		// 读取 node.readyc 通道
		// 该通道是 etcd-raft 组件与上层应用交互的主要channel之一
		// 其中传递的 Ready 实例也封装了很多信息
		case rd := <-rc.node.Ready():
			// 将当前 etcd raft 组件的状态信息，以及待持久化的 Entry 记录先记录到 WAL 日志文件中，
			// 即使之后宕机，这些信息也可以在节点下次启动时，通过前面回放 WAL 日志的方式进行恢复
			rc.wal.Save(rd.HardState, rd.Entries)
			// 检测到 etcd-raft 组件生成了新的快照数据
			if !raft.IsEmptySnap(rd.Snapshot) {
				// 将新的快照数据写入快照文件中
				rc.saveSnap(rd.Snapshot)
				// 将新快照持久化到 raftStorage
				rc.raftStorage.ApplySnapshot(rd.Snapshot)
				// 通知上层应用加载新快照
				rc.publishSnapshot(rd.Snapshot)
			}
			// 将待持久化的 Entry 记录追加到 raftStorage 中完成持久化
			rc.raftStorage.Append(rd.Entries)
			// 将待发送的消息发送到指定节点
			rc.transport.Send(rd.Messages)
			// 将已提交、待应用的 Entry 记录应用到上层应用的状态机中
			applyDoneC, ok := rc.publishEntries(rc.entriesToApply(rd.CommittedEntries))
			if !ok {
				rc.stop()
				return
			}

			// 随着节点的运行， WAL 日志量和 raftLog.storage 中的 Entry 记录会不断增加 ，
			// 所以节点每处理 10000 条(默认值) Entry 记录，就会触发一次创建快照的过程，
			// 同时 WAL 会释放一些日志文件的句柄，raftLog.storage 也会压缩其保存的 Entry 记录
			rc.maybeTriggerSnapshot(applyDoneC)
			// 上层应用处理完该 Ready 实例，通知 etcd-raft 纽件准备返回下一个 Ready 实例
			rc.node.Advance()

		case err := <-rc.transport.ErrorC:
			rc.writeError(err)
			return

		case <-rc.stopc:
			rc.stop()
			return
		}
	}
}
```

### 领导者选举

### 启动并初始化node节点

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

func NewRawNode(config *Config) (*RawNode, error) {
	// 这里调用初始化newRaft
	r := newRaft(config)
	rn := &RawNode{
		raft: r,
	}
	rn.prevSoftSt = r.softState()
	rn.prevHardSt = r.hardState()
	return rn, nil
}

func newRaft(c *Config) *raft {
	...
	r := &raft{
		id:                        c.ID,
		lead:                      None,
		isLearner:                 false,
		raftLog:                   raftlog,
		maxMsgSize:                c.MaxSizePerMsg,
		maxUncommittedSize:        c.MaxUncommittedEntriesSize,
		prs:                       tracker.MakeProgressTracker(c.MaxInflightMsgs),
		electionTimeout:           c.ElectionTick,
		heartbeatTimeout:          c.HeartbeatTick,
		logger:                    c.Logger,
		checkQuorum:               c.CheckQuorum,
		preVote:                   c.PreVote,
		readOnly:                  newReadOnly(c.ReadOnlyOption),
		disableProposalForwarding: c.DisableProposalForwarding,
	}

	...
	// 启动都是follower状态
	r.becomeFollower(r.Term, None)

	var nodesStrs []string
	for _, n := range r.prs.VoterNodes() {
		nodesStrs = append(nodesStrs, fmt.Sprintf("%x", n))
	}

	r.logger.Infof("newRaft %x [peers: [%s], term: %d, commit: %d, applied: %d, lastindex: %d, lastterm: %d]",
		r.id, strings.Join(nodesStrs, ","), r.Term, r.raftLog.committed, r.raftLog.applied, r.raftLog.lastIndex(), r.raftLog.lastTerm())
	return r
}
```

总结：  

进行node节点初始化工作，所有的Node开始都被初始化为Follower状态  

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

总结：  

Step是etcd-raft模块负责各类信息的入口  

default后面的step，被实现为一个状态机，它的step属性是一个函数指针，根据当前节点的不同角色，指向不同的消息处理函数：`stepLeader/stepFollower/stepCandidate`。与它类似的还有一个tick函数指针，根据角色的不同，也会在tickHeartbeat和tickElection之间来回切换，分别用来触发定时心跳和选举检测。  

### 发送心跳包

#### 作为leader

当一个节点成为leader的时候，会将节点的定时器设置为tickHeartbeat，然后周期性的调用，维持leader的地位  

```go
func (r *raft) becomeLeader() {
	// 检测当 前节点的状态，禁止从 follower 状态切换成 leader 状态
	if r.state == StateFollower {
		panic("invalid transition [follower -> leader]")
	}
	// 将step 字段设置成 stepLeader
	r.step = stepLeader
	r.reset(r.Term)
	// 设置心跳的函数
	r.tick = r.tickHeartbeat
	// 设置lead的id值
	r.lead = r.id
	// 更新当前的角色
	r.state = StateLeader
	...
}

func (r *raft) tickHeartbeat() {
	// 递增心跳计数器
	r.heartbeatElapsed++
	// 递增选举计数器
	r.electionElapsed++
	...

	if r.electionElapsed >= r.electionTimeout {
		r.electionElapsed = 0
		// 检测当前节点时候大多数节点保持连通
		if r.checkQuorum {
			r.Step(pb.Message{From: r.id, Type: pb.MsgCheckQuorum})
		}
		// If current leader cannot transfer leadership in electionTimeout, it becomes leader again.
		if r.state == StateLeader && r.leadTransferee != None {
			r.abortLeaderTransfer()
		}
	}

	if r.heartbeatElapsed >= r.heartbeatTimeout {
		r.heartbeatElapsed = 0
		r.Step(pb.Message{From: r.id, Type: pb.MsgBeat})
	}
}
```

becomeLeader中的step被设置成stepLeader，所以将会调用stepLeader来处理leader中对应的消息  

通过调用bcastHeartbeat向所有的节点发送心跳  

```go
func stepLeader(r *raft, m pb.Message) error {
	// These message types do not require any progress for m.From.
	switch m.Type {
	case pb.MsgBeat:
		// 向所有节点发送心跳
		r.bcastHeartbeat()
		return nil
	case pb.MsgCheckQuorum:
		// 检测是否和大部分节点保持连通
		// 如果不连通切换到follower状态
		if !r.prs.QuorumActive() {
			r.logger.Warningf("%x stepped down to follower since quorum is not active", r.id)
			r.becomeFollower(r.Term, None)
		}
		return nil
		...
	}
}

// bcastHeartbeat sends RPC, without entries to all the peers.
func (r *raft) bcastHeartbeat() {
	lastCtx := r.readOnly.lastPendingRequestCtx()
	// 这两个函数最终都将调用sendHeartbeat
	if len(lastCtx) == 0 {
		r.bcastHeartbeatWithCtx(nil)
	} else {
		r.bcastHeartbeatWithCtx([]byte(lastCtx))
	}
}

// 向指定的节点发送信息
func (r *raft) sendHeartbeat(to uint64, ctx []byte) {
	commit := min(r.prs.Progress[to].Match, r.raftLog.committed)
	m := pb.Message{
		To:      to,
		// 发送MsgHeartbeat类型的数据
		Type:    pb.MsgHeartbeat,
		Commit:  commit,
		Context: ctx,
	}

	r.send(m)
}
```

最终的心跳通过MsgHeartbeat的消息类型进行发送，通知它们目前Leader的存活状态，重置所有Follower持有的超时计时器  

#### 作为follower

1、接收到来自leader的RPC消息MsgHeartbeat；  

2、然后重置当前节点的选举超时时间；

3、回复leader自己的存活。

```go
func stepFollower(r *raft, m pb.Message) error {
	switch m.Type {
	case pb.MsgProp:
		...
	case pb.MsgHeartbeat:
		r.electionElapsed = 0
		r.lead = m.From
		r.handleHeartbeat(m)
		...
	}
	return nil
}

func (r *raft) handleHeartbeat(m pb.Message) {
	r.raftLog.commitTo(m.Commit)
	r.send(pb.Message{To: m.From, Type: pb.MsgHeartbeatResp, Context: m.Context})
}
```

#### 作为candidate

candidate来处理MsgHeartbeat的信息，是先把自己变成follower，然后和上面的follower一样，回复leader自己的存活。  

```go
func stepCandidate(r *raft, m pb.Message) error {
	...
	switch m.Type {
		...
	case pb.MsgHeartbeat:
		r.becomeFollower(m.Term, m.From) // always m.Term == r.Term
		r.handleHeartbeat(m)
	}
	...
	return nil
}

func (r *raft) handleHeartbeat(m pb.Message) {
	r.raftLog.commitTo(m.Commit)
	r.send(pb.Message{To: m.From, Type: pb.MsgHeartbeatResp, Context: m.Context})
}
```

当leader收到返回的信息的时候，会将对应的节点设置为RecentActive，表示该节点目前存活  

```go
func stepLeader(r *raft, m pb.Message) error {
	...
	// 根据from，取出当前的follower的Progress
	pr := r.prs.Progress[m.From]
	if pr == nil {
		r.logger.Debugf("%x no progress available for %x", r.id, m.From)
		return nil
	}
	switch m.Type {
	case pb.MsgHeartbeatResp:
		pr.RecentActive = true
		...
	}
	return nil
}
```

如果follower在一定的时间内，没有收到leader节点的消息，就会发起新一轮的选举，重新选一个leader节点  

### leader选举

#### 1、接收leader的心跳

```go
func (r *raft) becomeFollower(term uint64, lead uint64) {
	r.step = stepFollower
	r.reset(term)
	r.tick = r.tickElection
	r.lead = lead
	r.state = StateFollower
	r.logger.Infof("%x became follower at term %d", r.id, r.Term)
}

// follower以及candidate的tick函数，在r.electionTimeout之后被调用
func (r *raft) tickElection() {
	r.electionElapsed++
	// promotable返回是否可以被提升为leader
	// pastElectionTimeout检测当前的候选超时间是否过期
	if r.promotable() && r.pastElectionTimeout() {
		r.electionElapsed = 0
		// 发起选举
		r.Step(pb.Message{From: r.id, Type: pb.MsgHup})
	}
}
```

总结：  

1、如果可以成为leader;  

2、没有收到leader的心跳，候选超时时间过期了；  

3、重新发起新的选举请求。  

#### 2、发起竞选

Step函数看到MsgHup这个消息后会调用campaign函数，进入竞选状态

```go
func (r *raft) Step(m pb.Message) error {
	//...
	switch m.Type {
	case pb.MsgHup:
		if r.preVote {
			r.hup(campaignPreElection)
		} else {
			r.hup(campaignElection)
		}
	}
}

func (r *raft) hup(t CampaignType) {
	...
	r.campaign(t)
}

func (r *raft) campaign(t CampaignType) {
	...
	if t == campaignPreElection {
		r.becomePreCandidate()
		voteMsg = pb.MsgPreVote
		// PreVote RPCs are sent for the next term before we've incremented r.Term.
		term = r.Term + 1
	} else {
		// 切换到Candidate状态
		r.becomeCandidate()
		voteMsg = pb.MsgVote
		term = r.Term
	}
	// 统计当前节点收到的选票 并统计其得票数是否超过半数，这次检测主要是为单节点设置的
	// 判断是否是单节点
	if _, _, res := r.poll(r.id, voteRespMsgType(voteMsg), true); res == quorum.VoteWon {
		if t == campaignPreElection {
			r.campaign(campaignElection)
		} else {
			// 是单节点直接，变成leader
			r.becomeLeader()
		}
		return
	}
	...
	// 向集群中的所有节点发送信息，请求投票
	for _, id := range ids {
		// 跳过自身的节点
		if id == r.id {
			continue
		}
		r.logger.Infof("%x [logterm: %d, index: %d] sent %s request to %x at term %d",
			r.id, r.raftLog.lastTerm(), r.raftLog.lastIndex(), voteMsg, id, r.Term)

		var ctx []byte
		if t == campaignTransfer {
			ctx = []byte(t)
		}
		r.send(pb.Message{Term: term, To: id, Type: voteMsg, Index: r.raftLog.lastIndex(), LogTerm: r.raftLog.lastTerm(), Context: ctx})
	}
}
```

总结：  

主要是切换到campaign状态，然后将自己的term信息发送出去，请求投票。   

**这里我们能看到对于Candidate会有一个PreCandidate，PreCandidate这个状态的作用的是什么呢？**  

当系统曾出现分区，分区消失后恢复的时候，可能会造成某个被split的Follower的Term数值很大。  

对服务器进行分区时，它将不会收到heartbeat包，每次electionTimeout后成为Candidate都会递增Term。  

当服务器在一段时间后恢复连接时，Term的值将会变得很大，然后引入的重新选举会导致导致临时的延迟与可用性问题。   

PreElection阶段并不会真正增加当前节点的Term，它的主要作用是得到当前集群能否成功选举出一个Leader的答案，避免上面这种情况的发生。   

接着Candidate的状态来分析  

#### 3、其他节点收到信息，进行投票

能够投票需要满足下面条件：  

1、当前节点没有给任何节点投票 或 投票的节点term大于本节点的 或者 是之前已经投票的节点；

2、该节点的消息是最新的；    

```go
func (r *raft) Step(m pb.Message) error {
	...
	switch m.Type {
	case pb.MsgVote, pb.MsgPreVote:
		// We can vote if this is a repeat of a vote we've already cast...
		canVote := r.Vote == m.From ||
			// ...we haven't voted and we don't think there's a leader yet in this term...
			(r.Vote == None && r.lead == None) ||
			// ...or this is a PreVote for a future term...
			(m.Type == pb.MsgPreVote && m.Term > r.Term)
		// ...and we believe the candidate is up to date.
		if canVote && r.raftLog.isUpToDate(m.Index, m.LogTerm) {
			// 如果当前没有给任何节点投票（r.Vote == None）或者投票的节点term大于本节点的（m.Term > r.Term）
			// 或者是之前已经投票的节点（r.Vote == m.From）
			// 同时还满足该节点的消息是最新的（r.raftLog.isUpToDate(m.Index, m.LogTerm)），那么就接收这个节点的投票
			r.logger.Infof("%x [logterm: %d, index: %d, vote: %x] cast %s for %x [logterm: %d, index: %d] at term %d",
				r.id, r.raftLog.lastTerm(), r.raftLog.lastIndex(), r.Vote, m.Type, m.From, m.LogTerm, m.Index, r.Term)
			r.send(pb.Message{To: m.From, Term: m.Term, Type: voteRespMsgType(m.Type)})
			if m.Type == pb.MsgVote {
				// 保存下来给哪个节点投票了
				r.electionElapsed = 0
				r.Vote = m.From
			}
		} else {
			r.logger.Infof("%x [logterm: %d, index: %d, vote: %x] rejected %s from %x [logterm: %d, index: %d] at term %d",
				r.id, r.raftLog.lastTerm(), r.raftLog.lastIndex(), r.Vote, m.Type, m.From, m.LogTerm, m.Index, r.Term)
			r.send(pb.Message{To: m.From, Term: r.Term, Type: voteRespMsgType(m.Type), Reject: true})
		}

		...
	}
	return nil
}
```

#### 4、candidate节点统计投票的结果

candidate节点接收到投票的信息，然后统计投票的数量  

1、如果投票数大于节点数的一半，成为leader；  

2、如果达不到，变成follower；  

```go
func stepCandidate(r *raft, m pb.Message) error {
	// Only handle vote responses corresponding to our candidacy (while in
	// StateCandidate, we may get stale MsgPreVoteResp messages in this term from
	// our pre-candidate state).
	var myVoteRespType pb.MessageType
	if r.state == StatePreCandidate {
		myVoteRespType = pb.MsgPreVoteResp
	} else {
		myVoteRespType = pb.MsgVoteResp
	}
	switch m.Type {
	case myVoteRespType:
		// 计算当前集群中有多少节点给自己投了票
		gr, rj, res := r.poll(m.From, m.Type, !m.Reject)
		r.logger.Infof("%x has received %d %s votes and %d vote rejections", r.id, gr, m.Type, rj)
		switch res {
		// 大多数投票了
		case quorum.VoteWon:
			if r.state == StatePreCandidate {
				r.campaign(campaignElection)
			} else {
				// 如果进行投票的节点数量正好是半数以上节点数量
				r.becomeLeader()
				// 向集群中其他节点广 MsgApp 消息
				r.bcastAppend()
			}
			// 票数不够
		case quorum.VoteLost:
			// pb.MsgPreVoteResp contains future term of pre-candidate
			// m.Term > r.Term; reuse r.Term
			// 切换到follower
			r.becomeFollower(r.Term, None)
		}
		...
	}
	return nil
}
```

每当收到一个MsgVoteResp类型的消息时，就会设置当前节点持有的votes数组，更新其中存储的节点投票状态，如果收到大多数的节点票数，切换成leader，向其他的节点发送当前节点当选的消息，通知其余节点更新Raft结构体中的Term等信息。  


上面涉及到几种状态的切换  

正常情况只有3种状态    

<img src="/img/etcd-raft-node.png" alt="etcd" align=center/>

为了防止在分区的情况下，某个split的Follower的Term数值变得很大的场景，引入了PreCandidate  

<img src="/img/etcd-raft-node-pre.png" alt="etcd" align=center/>

对于不同节点之间的切换，调用的对应的bacome*方法就可以了  

这里需要注意的就是对应的每个bacome*中的step和tick

```go
func (r *raft) becomeFollower(term uint64, lead uint64) {
	r.step = stepFollower
	r.reset(term)
	r.tick = r.tickElection
	r.lead = lead
	r.state = StateFollower
	r.logger.Infof("%x became follower at term %d", r.id, r.Term)
}

func (r *raft) becomeCandidate() {
	// TODO(xiangli) remove the panic when the raft implementation is stable
	if r.state == StateLeader {
		panic("invalid transition [leader -> candidate]")
	}
	r.step = stepCandidate
	r.reset(r.Term + 1)
	r.tick = r.tickElection
	r.Vote = r.id
	r.state = StateCandidate
	r.logger.Infof("%x became candidate at term %d", r.id, r.Term)
}
```

它的step属性是一个函数指针，根据当前节点的不同角色，指向不同的消息处理函数：stepLeader/stepFollower/stepCandidate。  

tick也是一个函数指针，根据角色的不同，也会在tickHeartbeat和tickElection之间来回切换，分别用来触发定时心跳和选举检测。

### 日志同步

#### WAL日志

WAL（Write Ahead Log）最大的作用是记录了整个数据变化的全部历程。在etcd中，所有数据的修改在提交前，都要先写入到WAL中。使用WAL进行数据的存储使得etcd拥有两个重要功能。  

- 故障快速恢复： 当你的数据遭到破坏时，就可以通过执行所有WAL中记录的修改操作，快速从最原始的数据恢复到数据损坏前的状态。

- 数据回滚（undo）/重做（redo）：因为所有的修改操作都被记录在WAL中，需要回滚或重做，只需要反向或正向执行日志中的操作即可。

这里发放一张关于etcd中处理Entry记录的流程图(图片摘自【etcd技术内幕】)

<img src="/img/etcd-raft-wal.jpg" alt="etcd" align=center/>

具体的流程：  

- 1、客户端向etcd集群发起一次请求，请求中封装的Entry首先会交给etcd-raft处理，etcd-raft会将Entry记录保存到raftLog.unstable中；  

- 2、etcd-raft将Entry记录封装到Ready实例中，返回给上层模块进行持久化；  

- 3、上层模块收到持久化的Ready记录之后，会记录到WAL文件中，然后进行持久化，最后通知etcd-raft模块进行处理；  

- 4、etcd-raft将该Entry记录从unstable中移到storage中保存；  

- 5、当该Entry记录被复制到集区中的半数以上节点的时候，该Entry记录会被Lader节点认为是已经提交了，封装到Ready实例中通知上层模块；  

- 6、此时上层模块将该Ready实例封装的Entry记录应用到状态机中。   


#### leader同步follower日志

这里放一张etcd中leader节点同步数据到follower的流程图  

![avatar][base64str]




### 参考

【etcd技术内幕】一本关于etcd不错的书籍
【高可用分布式存储 etcd 的实现原理】https://draveness.me/etcd-introduction/  
【Raft 在 etcd 中的实现】https://blog.betacat.io/post/raft-implementation-in-etcd/  
【etcd Raft库解析】https://www.codedump.info/post/20180922-etcd-raft/  
【etcd raft 设计与实现《一》】https://zhuanlan.zhihu.com/p/51063866    
【raftexample 源码解读】https://zhuanlan.zhihu.com/p/91314329  
【etcd实现-全流程分析】https://zhuanlan.zhihu.com/p/135891186    
【线性一致性和Raft】https://pingcap.com/zh/blog/linearizability-and-raft  
【etcd raft 设计与实现《二》】https://zhuanlan.zhihu.com/p/51065416  


[base64str]:data:image/jpg;base64,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w8TFxsfIycrS09TV1tfY2drh4uPk5ebn6Onq8fLz9PX29/j5+v/EAB8BAAMBAQEBAQEBAQEAAAAAAAABAgMEBQYHCAkKC//EALURAAIBAgQEAwQHBQQEAAECdwABAgMRBAUhMQYSQVEHYXETIjKBCBRCkaGxwQkjM1LwFWJy0QoWJDThJfEXGBkaJicoKSo1Njc4OTpDREVGR0hJSlNUVVZXWFlaY2RlZmdoaWpzdHV2d3h5eoKDhIWGh4iJipKTlJWWl5iZmqKjpKWmp6ipqrKztLW2t7i5usLDxMXGx8jJytLT1NXW19jZ2uLj5OXm5+jp6vLz9PX29/j5+v/bAEMAAgICAgICAwICAwUDAwMFBgUFBQUGCAYGBgYGCAoICAgICAgKCgoKCgoKCgwMDAwMDA4ODg4ODw8PDw8PDw8PD//bAEMBAgICBAQEBwQEBxALCQsQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEP/dAAQAX//aAAwDAQACEQMRAD8A/fujFLRQAmBRgUtFACUYpaKAEwKMClooASjFLRQAmBRgUtFACUYpaKAEwKMClooASjFLRQAmBRgUtFACUYpaKAEwKMClooASgDilpBQAYFGBS0UAJRilooATAowKWigBKMUtFACYFGBS0UAJRilooATAowKWigBMCjFFLQAmBRgUtFACUYpaKAEwKMClooASjFLRQAmBRgUtJQAUYpaKAEwKMClooASjFLRQAmBRgUtFACUYpaKAEwKMClooASjFLRQAmBmjAo70tACUYpaKAEwKMClooASjFLRQAmBRgUtFACUYpaKAEwKMClooASjHFLSGgAwKMClooASjFLRQAmBRgUtFACUYpaKAEwKMClooATAoxRS0AJgUYFLRQAlGKWigBMCjApaKAEoxS0UAJgUYFLRQAlGKWigBMCjApaKAEGMUYoFLQAmBRgUtFACUYpaKAEwKMClooASjFLRQAmBRgUtFACUYpaKAEwKMClooASjFLRQAmBRgUtFACUYpaKAEwKMClooASjFLRQAmBRgUUtACUYpaKAEwKMClooASjFLRQAmBRgUtFACUYpaKAEwKMClooASjFLSd6ADAowKWigBKMUtFACYFGBS0UAJRilooATAowKWigBKMUd6WgBMCjApaKAEo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
  












