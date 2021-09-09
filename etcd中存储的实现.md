<!-- START doctoc generated TOC please keep comment here to allow auto update -->
<!-- DON'T EDIT THIS SECTION, INSTEAD RE-RUN doctoc TO UPDATE -->

- [etcd中的存储实现](#etcd%E4%B8%AD%E7%9A%84%E5%AD%98%E5%82%A8%E5%AE%9E%E7%8E%B0)
  - [前言](#%E5%89%8D%E8%A8%80)
  - [V3和V2版本的对比](#v3%E5%92%8Cv2%E7%89%88%E6%9C%AC%E7%9A%84%E5%AF%B9%E6%AF%94)
  - [MVCC](#mvcc)
    - [treeIndex 原理](#treeindex-%E5%8E%9F%E7%90%86)
    - [MVCC 更新 key](#mvcc-%E6%9B%B4%E6%96%B0-key)
    - [MVCC 查询 key](#mvcc-%E6%9F%A5%E8%AF%A2-key)
    - [MVCC 删除 key](#mvcc-%E5%88%A0%E9%99%A4-key)
  - [boltdb 存储](#boltdb-%E5%AD%98%E5%82%A8)
    - [只读事务](#%E5%8F%AA%E8%AF%BB%E4%BA%8B%E5%8A%A1)
    - [读写事务](#%E8%AF%BB%E5%86%99%E4%BA%8B%E5%8A%A1)
  - [参考](#%E5%8F%82%E8%80%83)

<!-- END doctoc generated TOC please keep comment here to allow auto update -->

## etcd中的存储实现

### 前言

前面了关于etcd的raft相关的实现，这里来看下存储的相关实现  

通过看etcd的代码我们可以看到目前有两个大的版本，v2和v3。

### V3和V2版本的对比

etcd的v2版本有下面的一些问题  

**功能局限性**  

1、etcd v2 不支持范围查询和分页；  

2、etcd v2 不支持多 key 事务；  

**Watch 机制可靠性问题**  

etcd v2 是内存型、不支持保存 key 历史版本的数据库，只在内存中使用滑动窗口保存了最近的 1000 条变更事件，当 etcd server 写请求较多、网络波动时等场景，很容易出现事件丢失问题，进而又触发 client 数据全量拉取，产生大量 expensive request，甚至导致 etcd 雪崩。  

**性能瓶颈问题**

1、etcd v2早起使用的是 HTTP/1.x API。HTTP/1.x 协议没有压缩机制，大量的请求可能导致 etcd 出现 CPU 高负载、OOM、丢包等问题；  

2、etcd v2 client 会通过 HTTP 长连接轮询 Watch 事件，当 watcher 较多的时候，因 HTTP/1.x 不支持多路复用，会创建大量的连接，消耗 server 端过多的 socket 和内存资源；  

3、对于 key 中的 TTL过期时间，如果大量 key TTL 一样，也需要分别为每个 key 发起续期操作，当 key 较多的时候，这会显著增加集群负载、导致集群性能显著下降；  

`内存开销问题`

etcd v2 在内存维护了一颗树来保存所有节点 key 及 value。在数据量场景略大的场景，如配置项较多、存储了大量 Kubernetes Events， 它会导致较大的内存开销，同时 etcd 需要定时把全量内存树持久化到磁盘。这会消耗大量的 CPU 和磁盘 I/O 资源，对系统的稳定性造成一定影响。  

etcd v3 的出现就是为了解决以上稳定性、扩展性、性能问题  

1、在内存开销、Watch 事件可靠性、功能局限上，它通过引入 B-tree、boltdb 实现一个 MVCC 数据库，数据模型从层次型目录结构改成扁平的 key-value，提供稳定可靠的事件通知，实现了事务，支持多 key 原子更新，同时基于 boltdb 的持久化存储，显著降低了 etcd 的内存占用、避免了 etcd v2 定期生成快照时的昂贵的资源开销；    

2、etcd v3 使用了 gRPC API，使用 protobuf 定义消息，消息编解码性能相比 JSON 超过 2 倍以上，并通过 HTTP/2.0 多路复用机制，减少了大量 watcher 等场景下的连接数；   

3、使用 Lease 优化 TTL 机制，每个 Lease 具有一个 TTL，相同的 TTL 的 key 关联一个 Lease，Lease 过期的时候自动删除相关联的所有 key，不再需要为每个 key 单独续期；  

4、etcd v3 支持范围、分页查询，可避免大包等 expensive request。  

### MVCC 

MVCC 机制正是基于多版本技术实现的一种乐观锁机制，它乐观地认为数据不会发生冲突，但是当事务提交时，具备检测数据是否冲突的能力。  

在 MVCC 数据库中，你更新一个 key-value 数据的时候，它并不会直接覆盖原数据，而是新增一个版本来存储新的数据，每个数据都有一个版本号，版本号是一个逻辑时钟，不会因为服务器时间的差异而受影响。  

上面讲到了乐观锁，我们来再来复习下悲观锁是什么意思？  

悲观锁是一种事先预防机制，它悲观地认为多个并发事务可能会发生冲突，因此它要求事务必须先获得锁，才能进行修改数据操作。但是悲观锁粒度过大、高并发场景下大量事务会阻塞等，会导致服务性能较差。   

#### treeIndex 原理

在 treeIndex 中，每个节点的 key 是一个 keyIndex 结构，etcd 就是通过它保存了用户的 key 与版本号的映射关系。  

来看下keyIndex数据结构    

```go
// etcd/server/mvcc/key_index.go
type keyIndex struct {
	key         []byte // 用户的key名称
	modified    revision // 最后一次修改key时的etcd版本号
	generations []generation // generation保存了一个key若干代版本号信息，每代中包含对key的多次修改的版本号列表
}
```

generations 表示一个 key 从创建到删除的过程，每代对应 key 的一个生命周期的开始与结束。当你第一次创建一个 key 时，会生成第 0 代，后续的修改操作都是在往第 0 代中追加修改版本号。当你把 key 删除后，它就会生成新的第 1 代，一个 key 不断经历创建、删除的过程，它就会生成多个代。  

```go
// generation contains multiple revisions of a key.
type generation struct {
	ver     int64 // 表示此key的修改次数
	created revision // 表示generation结构创建时的版本号
	revs    []revision // 每次修改key时的revision追加到此数组
}
```

再来看下 revision  

```go
// A revision indicates modification of the key-value space.
// The set of changes that share same main revision changes the key-value space atomically.
type revision struct {
	// 一个全局递增的主版本号，随put/txn/delete事务递增，一个事务内的key main版本号是一致的
	main int64

	// 一个事务内的子版本号，从0开始随事务内put/delete操作递增
	sub int64
}
```

看完基本的数据结构，我们来看下 mvcc 对 key 的操作流程  

#### MVCC 更新 key

执行 put 操作的时候首先从 treeIndex 模块中查询 key 的 keyIndex 索引信息，keyIndex 在上面已经介绍了。  

- 如果首次操作，也就是 treeIndex 中找不到对应的，etcd 会根据当前的全局版本号（空集群启动时默认为 1）自增，生成 put 操作对应的版本号 revision{2,0}，这就是 boltdb 的 key。  

- 如果能找到，在当前的 keyIndex append 一个操作的 revision  

```go
// etcd/server/mvcc/index.go
func (ti *treeIndex) Put(key []byte, rev revision) {
	keyi := &keyIndex{key: key}

	ti.Lock()
	defer ti.Unlock()
	item := ti.tree.Get(keyi)
	// 没有找到
	if item == nil {
		keyi.put(ti.lg, rev.main, rev.sub)
		ti.tree.ReplaceOrInsert(keyi)
		return
	}
	okeyi := item.(*keyIndex)
	okeyi.put(ti.lg, rev.main, rev.sub)
}

// etcd/server/mvcc/key_index.go
// put puts a revision to the keyIndex.
func (ki *keyIndex) put(lg *zap.Logger, main int64, sub int64) {
	rev := revision{main: main, sub: sub}

	if len(ki.generations) == 0 {
		ki.generations = append(ki.generations, generation{})
	}
	g := &ki.generations[len(ki.generations)-1]
	if len(g.revs) == 0 { // create a new key
		keysGauge.Inc()
		g.created = rev
	}
	g.revs = append(g.revs, rev)
	g.ver++
	ki.modified = rev
}
```

填充完 treeIndex ，这时候就会将数据保存到 boltdb 的缓存中，并同步更新 buffer  

#### MVCC 查询 key

在读事务中，它首先需要根据 key 从 treeIndex 模块获取版本号，如果未带版本号，默认是读取最新的数据。treeIndex 模块从 B-tree 中，根据 key 查找到 keyIndex 对象后，匹配有效的 generation，返回 generation 的 revisions 数组中最后一个版本号给读事务对象。  

读事务对象根据此版本号为 key，通过 Backend 的并发读事务（ConcurrentReadTx）接口，优先从 buffer 中查询，命中则直接返回，否则从 boltdb 中查询此 key 的 value 信息。具体可参见下文的只读事务。  

当然上面是查找最新的数据，如果我们查询历史中的某一个版本的信息呢？  

处理过程是一样的，只不过是根据 key 从 treeIndex 模块获取版本号，不是获取最新的，而是获取小于等于 我们指定的版本号 的最大历史版本号，然后再去查询对应的值信息。   

```go
// etcd/server/mvcc/index.go
func (ti *treeIndex) Get(key []byte, atRev int64) (modified, created revision, ver int64, err error) {
	keyi := &keyIndex{key: key}
	ti.RLock()
	defer ti.RUnlock()
	if keyi = ti.keyIndex(keyi); keyi == nil {
		return revision{}, revision{}, 0, ErrRevisionNotFound
	}
	return keyi.get(ti.lg, atRev)
}

// get gets the modified, created revision and version of the key that satisfies the given atRev.
// Rev must be higher than or equal to the given atRev.
func (ki *keyIndex) get(lg *zap.Logger, atRev int64) (modified, created revision, ver int64, err error) {
	if ki.isEmpty() {
		lg.Panic(
			"'get' got an unexpected empty keyIndex",
			zap.String("key", string(ki.key)),
		)
	}
	g := ki.findGeneration(atRev)
	if g.isEmpty() {
		return revision{}, revision{}, 0, ErrRevisionNotFound
	}

	n := g.walk(func(rev revision) bool { return rev.main > atRev })
	if n != -1 {
		return g.revs[n], g.created, g.ver - int64(len(g.revs)-n-1), nil
	}

	return revision{}, revision{}, 0, ErrRevisionNotFound
}

// 找出给定的 rev 所属的 generation
func (ki *keyIndex) findGeneration(rev int64) *generation {
	lastg := len(ki.generations) - 1
	cg := lastg

	for cg >= 0 {
		if len(ki.generations[cg].revs) == 0 {
			cg--
			continue
		}
		g := ki.generations[cg]
		if cg != lastg {
			if tomb := g.revs[len(g.revs)-1].main; tomb <= rev {
				return nil
			}
		}
		if g.revs[0].main <= rev {
			return &ki.generations[cg]
		}
		cg--
	}
	return nil
}
```

关于从获取 key 的 value 信息的过程可参考下文的 只读事务。  

#### MVCC 删除 key  

再来看下删除的逻辑  

etcd 中的删除操作，是延期删除模式，和更新 key 类似  

相比更新操作：  

1、生成的 boltdb key 版本号追加了删除标识（tombstone, 简写 t），boltdb value 变成只含用户 key 的 KeyValue 结构体；  

2、treeIndex 模块也会给此 key hello 对应的 keyIndex 对象，追加一个空的 generation 对象，表示此索引对应的 key 被删除了；   

当你再次查询对应 key 的时候，treeIndex 模块根据 key 查找到 keyindex 对象后，若发现其存在空的 generation 对象，并且查询的版本号大于等于被删除时的版本号，则会返回空。    

那么 key 打上删除标记后有哪些用途呢？什么时候会真正删除它呢？  

一方面删除 key 时会生成 events，Watch 模块根据 key 的删除标识，会生成对应的 Delete 事件。  

另一方面，当你重启 etcd，遍历 boltdb 中的 key 构建 treeIndex 内存树时，你需要知道哪些 key 是已经被删除的，并为对应的 key 索引生成 tombstone 标识。而真正删除 treeIndex 中的索引对象、boltdb 中的 key 是通过压缩 (compactor) 组件异步完成。  

正因为 etcd 的删除 key 操作是基于以上延期删除原理实现的，因此只要压缩组件未回收历史版本，我们就能从 etcd 中找回误删的数据。  



### boltdb 存储

下来看下 Backend 的细节， etcd 中通过 Backend，很好的封装了存储引擎的实现细节，为上层提供一个更一致的接口，方便了 etcd 中其他模块的使用  

```go
// etcd/server/mvcc/backend/backend.go
type Backend interface {
	// ReadTx 返回一个读事务。它被主数据路径中的 ConcurrentReadTx 替换
	ReadTx() ReadTx
	BatchTx() BatchTx
	// ConcurrentReadTx returns a non-blocking read transaction.
	ConcurrentReadTx() ReadTx

	Snapshot() Snapshot
	Hash(ignores func(bucketName, keyName []byte) bool) (uint32, error)
	// Size 返回后端物理分配的当前大小。
	Size() int64
	// SizeInUse 返回逻辑上正在使用的后端的当前大小。
	SizeInUse() int64
	OpenReadTxN() int64
	Defrag() error
	ForceCommit()
	Close() error
}
```

再来看下 pacakge 内部的 backend 结构体，这是一个实现了 Backend 接口的结构：  

```go
// etcd/server/mvcc/backend/backend.go
type backend struct {
	// size and commits are used with atomic operations so they must be
	// 64-bit aligned, otherwise 32-bit tests will crash

	// size is the number of bytes allocated in the backend
	size int64
	// sizeInUse is the number of bytes actually used in the backend
	sizeInUse int64
	// commits counts number of commits since start
	commits int64
	// openReadTxN is the number of currently open read transactions in the backend
	openReadTxN int64
	// mlock prevents backend database file to be swapped
	mlock bool

	mu sync.RWMutex
	db *bolt.DB

	// 默认100ms
	batchInterval time.Duration
	// 默认defaultBatchLimit    = 10000
	batchLimit    int
	batchTx       *batchTxBuffered

	readTx *readTx
	// txReadBufferCache mirrors "txReadBuffer" within "readTx" -- readTx.baseReadTx.buf.
	// When creating "concurrentReadTx":
	// - if the cache is up-to-date, "readTx.baseReadTx.buf" copy can be skipped
	// - if the cache is empty or outdated, "readTx.baseReadTx.buf" copy is required
	txReadBufferCache txReadBufferCache

	stopc chan struct{}
	donec chan struct{}

	hooks Hooks

	lg *zap.Logger
}
```

readTx 和 batchTx 分别实现了 ReadTx 和 BatchTx 接口，其中 readTx 负责读请求，batchTx 负责写请求  

```go
// etcd/server/mvcc/backend/read_tx.go
type ReadTx interface {
	Lock()
	Unlock()
	RLock()
	RUnlock()

	UnsafeRange(bucket Bucket, key, endKey []byte, limit int64) (keys [][]byte, vals [][]byte)
	UnsafeForEach(bucket Bucket, visitor func(k, v []byte) error) error
}

// etcd/server/mvcc/backend/batch_tx.go
type BatchTx interface {
	ReadTx
	UnsafeCreateBucket(bucket Bucket)
	UnsafeDeleteBucket(bucket Bucket)
	UnsafePut(bucket Bucket, key []byte, value []byte)
	UnsafeSeqPut(bucket Bucket, key []byte, value []byte)
	UnsafeDelete(bucket Bucket, key []byte)
	// Commit commits a previous tx and begins a new writable one.
	Commit()
	// CommitAndStop commits the previous tx and does not create a new one.
	CommitAndStop()
}
```

readTx 和 batchTx 的创建在 newBackend 中完成  

```go
func newBackend(bcfg BackendConfig) *backend {
	if bcfg.Logger == nil {
		bcfg.Logger = zap.NewNop()
	}

	bopts := &bolt.Options{}
	if boltOpenOptions != nil {
		*bopts = *boltOpenOptions
	}
	bopts.InitialMmapSize = bcfg.mmapSize()
	bopts.FreelistType = bcfg.BackendFreelistType
	bopts.NoSync = bcfg.UnsafeNoFsync
	bopts.NoGrowSync = bcfg.UnsafeNoFsync
	bopts.Mlock = bcfg.Mlock

	db, err := bolt.Open(bcfg.Path, 0600, bopts)
	if err != nil {
		bcfg.Logger.Panic("failed to open database", zap.String("path", bcfg.Path), zap.Error(err))
	}

	// In future, may want to make buffering optional for low-concurrency systems
	// or dynamically swap between buffered/non-buffered depending on workload.
	b := &backend{
		db: db,

		batchInterval: bcfg.BatchInterval,
		batchLimit:    bcfg.BatchLimit,
		mlock:         bcfg.Mlock,

		readTx: &readTx{
			baseReadTx: baseReadTx{
				buf: txReadBuffer{
					txBuffer:   txBuffer{make(map[BucketID]*bucketBuffer)},
					bufVersion: 0,
				},
				buckets: make(map[BucketID]*bolt.Bucket),
				txWg:    new(sync.WaitGroup),
				txMu:    new(sync.RWMutex),
			},
		},
		txReadBufferCache: txReadBufferCache{
			mu:         sync.Mutex{},
			bufVersion: 0,
			buf:        nil,
		},

		stopc: make(chan struct{}),
		donec: make(chan struct{}),

		lg: bcfg.Logger,
	}

	b.batchTx = newBatchTxBuffered(b)
	// We set it after newBatchTxBuffered to skip the 'empty' commit.
	b.hooks = bcfg.Hooks

	go b.run()
	return b
}

func (b *backend) run() {
	defer close(b.donec)
	// 定时提交事务
	t := time.NewTimer(b.batchInterval)
	defer t.Stop()
	for {
		select {
		case <-t.C:
		case <-b.stopc:
			b.batchTx.CommitAndStop()
			return
		}
		if b.batchTx.safePending() != 0 {
			b.batchTx.Commit()
		}
		t.Reset(b.batchInterval)
	}
}
```

newBackend 在启动的时候会开启一个 goroutine ，定期的提交事务   

#### 只读事务

来看下只读事务的现实  

```go
// Base type for readTx and concurrentReadTx to eliminate duplicate functions between these
type baseReadTx struct {
	// 保护 txReadBuffer 的访问
	mu  sync.RWMutex
	buf txReadBuffer

	// 保护 tx
	txMu    *sync.RWMutex
	tx      *bolt.Tx
	buckets map[BucketID]*bolt.Bucket
	// txWg 保护 tx 在批处理间隔结束时不会被回滚，直到使用此 tx 的所有读取完成。
	txWg *sync.WaitGroup
}
```

可以引入了两把读写锁来保护相应的资源，除了用于保护 tx 的 txmu 读写锁之外，还存在另外一个 mu 读写锁，它的作用是保证 buf 中的数据不会出现问题，buf 和结构体中的 buckets 都是用于加速读效率的缓存。  

它对位提供了两个方法 UnsafeRange 和 UnsafeForEach  

UnsafeRange 从名字就可以答题推断出这个函数的作用就是做范围查询。  

在 etcd 中无论我们想要后去单个 Key 还是一个范围内的 Key 最终都是通过 Range 来实现的  

```go
// etcd/server/mvcc/backend/read_tx.go
func (baseReadTx *baseReadTx) UnsafeRange(bucketType Bucket, key, endKey []byte, limit int64) ([][]byte, [][]byte) {
	if endKey == nil {
		// forbid duplicates for single keys
		limit = 1
	}
	if limit <= 0 {
		limit = math.MaxInt64
	}
	if limit > 1 && !bucketType.IsSafeRangeBucket() {
		panic("do not use unsafeRange on non-keys bucket")
	}
    // 首先从缓存中查询键值对
	keys, vals := baseReadTx.buf.Range(bucketType, key, endKey, limit)
    // 检测缓存中返回的键值对是否达到Limit的要求，如果达到Limit的指定上限，直接返回缓存的查询结果
	if int64(len(keys)) == limit {
		return keys, vals
	}

	// find/cache bucket
	bn := bucketType.ID()
	baseReadTx.txMu.RLock()
	bucket, ok := baseReadTx.buckets[bn]
	baseReadTx.txMu.RUnlock()
	lockHeld := false
	if !ok {
		baseReadTx.txMu.Lock()
		lockHeld = true
		bucket = baseReadTx.tx.Bucket(bucketType.Name())
		baseReadTx.buckets[bn] = bucket
	}

	// ignore missing bucket since may have been created in this batch
	if bucket == nil {
		if lockHeld {
			baseReadTx.txMu.Unlock()
		}
		return keys, vals
	}
	if !lockHeld {
		baseReadTx.txMu.Lock()
	}
	c := bucket.Cursor()
	baseReadTx.txMu.Unlock()

    // 将查询缓存的结采与查询 BlotDB 的结果合并 然后返回
	k2, v2 := unsafeRange(c, key, endKey, limit-int64(len(keys)))
	return append(k2, keys...), append(v2, vals...)
}

func unsafeRange(c *bolt.Cursor, key, endKey []byte, limit int64) (keys [][]byte, vs [][]byte) {
	if limit <= 0 {
		limit = math.MaxInt64
	}
	var isMatch func(b []byte) bool
	if len(endKey) > 0 {
		isMatch = func(b []byte) bool { return bytes.Compare(b, endKey) < 0 }
	} else {
		isMatch = func(b []byte) bool { return bytes.Equal(b, key) }
		limit = 1
	}

	for ck, cv := c.Seek(key); ck != nil && isMatch(ck); ck, cv = c.Next() {
		vs = append(vs, cv)
		keys = append(keys, ck)
		if limit == int64(len(keys)) {
			break
		}
	}
	return keys, vs
}
```

梳理下流程：  

1、首先从 baseReadTx 的 buf 里面查询，如果从 buf 里面已经拿到了足够的 KV (入参里面有限制 range 查询的最大数量)，那么就直接返回拿到的 KVs;  
  
2、如果 buf 里面的KV不足以满足要求，那么这里就会利用 BoltDB 的读事务接口去 BoltDB 里面查询 KV，然后返回。  

#### 读写事务  

读写事务提供了读和写的数据的能力  

```go
type batchTx struct {
	sync.Mutex
	tx      *bolt.Tx
	backend *backend

	pending int
}
```

写数据的请求会调用 UnsafePut 写入数据到 BoltDB 中  

```go
// go.etcd.io/bbolt@v1.3.6/tx.go
// UnsafePut must be called holding the lock on the tx.
func (t *batchTx) UnsafePut(bucket Bucket, key []byte, value []byte) {
	t.unsafePut(bucket, key, value, false)
}

func (t *batchTx) unsafePut(bucketType Bucket, key []byte, value []byte, seq bool) {
	// 获取bucket的实例
	bucket := t.tx.Bucket(bucketType.Name())
	if bucket == nil {
		...
	}
	if seq {
		// 如果顺序写入，将填充率设置成90%
		bucket.FillPercent = 0.9
	}
	// 使用 BoltDB 的 put 写入数据
	if err := bucket.Put(key, value); err != nil {
        ...
	}
	t.pending++
}
```

数据存储到 BoltDB 中，BoltDB 本身提供了 Put 的写入 API  

UnsafeDelete 和这个差不多，跳过  

在执行 PUT 和 DELETE 之后，数据没有提交，我们还需要手动或者等待 etcd 自动将请求提交：  

```go
// etcd/server/mvcc/backend/batch_tx.go
// Commit commits a previous tx and begins a new writable one.
func (t *batchTx) Commit() {
	t.Lock()
	t.commit(false)
	t.Unlock()
}

func (t *batchTx) commit(stop bool) {
	// commit the last tx
	if t.tx != nil {
		// 前读写事务未做任何修改就无须开启新的事务
		if t.pending == 0 && !stop {
			return
		}

		start := time.Now()

		// 通过 BoltDB 提供的api提交当前的事务
		err := t.tx.Commit()
		// gofail: var afterCommit struct{}

		rebalanceSec.Observe(t.tx.Stats().RebalanceTime.Seconds())
		spillSec.Observe(t.tx.Stats().SpillTime.Seconds())
		writeSec.Observe(t.tx.Stats().WriteTime.Seconds())
		commitSec.Observe(time.Since(start).Seconds())
		// 增加 backend.commits 数量
		atomic.AddInt64(&t.backend.commits, 1)

		// 重置 pending 的数量
		t.pending = 0
		if err != nil {
			t.backend.lg.Fatal("failed to commit tx", zap.Error(err))
		}
	}
	if !stop {
		// 开启新的读写事务
		t.tx = t.backend.begin(true)
	}
}
```

事务的提交到这就介绍完了  





### 参考

【etcd Backend存储引擎实现原理】https://blog.csdn.net/u010853261/article/details/109630223    
【高可用分布式存储 etcd 的实现原理】https://draveness.me/etcd-introduction/    





