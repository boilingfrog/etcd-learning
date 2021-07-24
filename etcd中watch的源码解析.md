<!-- START doctoc generated TOC please keep comment here to allow auto update -->
<!-- DON'T EDIT THIS SECTION, INSTEAD RE-RUN doctoc TO UPDATE -->

- [etcd中watch的源码解析](#etcd%E4%B8%ADwatch%E7%9A%84%E6%BA%90%E7%A0%81%E8%A7%A3%E6%9E%90)
  - [前言](#%E5%89%8D%E8%A8%80)
  - [client端的代码](#client%E7%AB%AF%E7%9A%84%E4%BB%A3%E7%A0%81)
    - [Watch](#watch)
    - [newWatcherGrpcStream](#newwatchergrpcstream)
    - [run](#run)
    - [newWatchClient](#newwatchclient)
    - [serveSubstream](#servesubstream)
  - [server端的代码实现](#server%E7%AB%AF%E7%9A%84%E4%BB%A3%E7%A0%81%E5%AE%9E%E7%8E%B0)
    - [watchableStore](#watchablestore)
    - [syncWatchersLoop](#syncwatchersloop)
    - [syncWatchers](#syncwatchers)
    - [syncVictimsLoop](#syncvictimsloop)
    - [moveVictims](#movevictims)
    - [watchServer](#watchserver)
    - [recvLoop](#recvloop)
    - [sendLoop](#sendloop)
  - [连接复用](#%E8%BF%9E%E6%8E%A5%E5%A4%8D%E7%94%A8)
  - [总结](#%E6%80%BB%E7%BB%93)

<!-- END doctoc generated TOC please keep comment here to allow auto update -->

## etcd中watch的源码解析

### 前言

etcd是一个cs网络架构，源码分析应该涉及到client端，server端。client主要是提供操作来请求对key监听，并且接收key变更时的通知。server要能做到接收key监听请求，并且启动定时器等方法来对key进行监听，有变更时通知client。  

这里主要分析了v3版本的实现  

### client端的代码  

<img src="/img/etcd-watch-client.png" alt="etcd" align=center/>

#### Watch

client端的实现相对简单，我们主要来看下这个Watch的实现  

```go
// client/v3/watch.go

type Watcher interface {
	// 在键或前缀上监听。将监听的事件
	// 通过定义的返回的channel进行返回。如果修订等待通过
	// 监听被压缩，然后监听将被服务器取消，
	// 客户端将发布压缩的错误观察响应，并且通道将关闭。
	// 如果请求的修订为 0 或未指定，则返回的通道将
	// 返回服务器收到监视请求后发生的监视事件。
	// 如果上下文“ctx”被取消或超时，返回的“WatchChan”关闭，
	// 并且来自此关闭通道的“WatchResponse”具有零事件且为零“Err()”。
	// 一旦不再使用观察者，上下文“ctx”必须被取消，
	// 释放相关资源。
	//
	// 如果上下文是“context.Background/TODO”，则返回“WatchChan”
	// 不会被关闭和阻塞直到事件被触发，除非服务器
	// 返回一个不可恢复的错误（例如 ErrCompacted）。
	// 例如，当上下文通过“WithRequireLeader”和
	// 连接的服务器没有领导者（例如，由于网络分区），
	// 将返回错误“etcdserver: no leader”（ErrNoLeader），
	// 然后 "WatchChan" 以非零 "Err()" 关闭。
	// 为了防止观察流卡在分区节点中，
	// 确保使用“WithRequireLeader”包装上下文。
	//
	// 否则，只要上下文没有被取消或超时，
	// watch 将永远重试其他可恢复的错误，直到重新连接。
	//
	// TODO：在最后一个“WatchResponse”消息中显式设置上下文错误并关闭通道？
	// 目前，客户端上下文被永远不会关闭的“valCtx”覆盖。
	// TODO(v3.4): 配置watch重试策略，限制最大重试次数
	//（参见 https://github.com/etcd-io/etcd/issues/8980）
	Watch(ctx context.Context, key string, opts ...OpOption) WatchChan

	// RequestProgress requests a progress notify response be sent in all watch channels.
	RequestProgress(ctx context.Context) error

	// Close closes the watcher and cancels all watch requests.
	Close() error
}

// watcher implements the Watcher interface
type watcher struct {
	remote   pb.WatchClient
	callOpts []grpc.CallOption

	// mu protects the grpc streams map
	mu sync.Mutex

	// streams 保存所有由 ctx 值键控的活动 grpc 流。
	streams map[string]*watchGrpcStream
	lg      *zap.Logger
}

// watchGrpcStream 跟踪附加到单个 grpc 流的所有watch资源。
type watchGrpcStream struct {
	owner    *watcher
	remote   pb.WatchClient
	callOpts []grpc.CallOption

	// ctx 控制内部的remote.Watch requests
	ctx context.Context
	// ctxKey 用来找流的上下文信息
	ctxKey string
	cancel context.CancelFunc

	// substreams 持有此 grpc 流上的所有活动的watchers
	substreams map[int64]*watcherStream
	// 恢复保存此 grpc 流上的所有正在恢复的观察者
	resuming []*watcherStream

	// reqc 从 Watch() 向主协程发送观察请求
	reqc chan watchStreamRequest
	// respc 从 watch 客户端接收数据
	respc chan *pb.WatchResponse
	// donec 通知广播进行退出
	donec chan struct{}
	// errc transmits errors from grpc Recv to the watch stream reconnect logic
	errc chan error
	// Closec 获取关闭观察者的观察者流
	closingc chan *watcherStream
	// 当所有子流 goroutine 都退出时，wg 完成
	wg sync.WaitGroup

	// resumec 关闭以表示所有子流都应开始恢复
	resumec chan struct{}
	// closeErr 是关闭监视流的错误
	closeErr error

	lg *zap.Logger
}

// watcherStream 代表注册的观察者
// watch()时，构造watchgrpcstream时构造的watcherStream，用于封装一个watch rpc请求，包含订阅监听key，通知key变更通道，一些重要标志。
type watcherStream struct {
	// initReq 是发起这个请求的请求
	initReq watchRequest

	// outc 向订阅者发布watch响应
	outc chan WatchResponse
	// recvc buffers watch responses before publishing
	recvc chan *WatchResponse
	// 当 watcherStream goroutine 停止时 donec 关闭
	donec chan struct{}
	// 当应该安排流关闭时，closures 设置为 true。
	closing bool
	// id 是在 grpc 流上注册的 watch id
	id int64

	// buf 保存从 etcd 收到但尚未被客户端消费的所有事件
	buf []*WatchResponse
}

// Watch post一个watch请求，通过run()来监听watch新创建的watch通道，等待watch事件
func (w *watcher) Watch(ctx context.Context, key string, opts ...OpOption) WatchChan {
	ow := opWatch(key, opts...)

	var filters []pb.WatchCreateRequest_FilterType
	if ow.filterPut {
		filters = append(filters, pb.WatchCreateRequest_NOPUT)
	}
	if ow.filterDelete {
		filters = append(filters, pb.WatchCreateRequest_NODELETE)
	}

	wr := &watchRequest{
		ctx:            ctx,
		createdNotify:  ow.createdNotify,
		key:            string(ow.key),
		end:            string(ow.end),
		rev:            ow.rev,
		progressNotify: ow.progressNotify,
		fragment:       ow.fragment,
		filters:        filters,
		prevKV:         ow.prevKV,
		retc:           make(chan chan WatchResponse, 1),
	}

	ok := false
	ctxKey := streamKeyFromCtx(ctx)

	var closeCh chan WatchResponse
	for {
		// 查找或分配适当的 grpc 监视流
		w.mu.Lock()
		if w.streams == nil {
			// closed
			w.mu.Unlock()
			ch := make(chan WatchResponse)
			close(ch)
			return ch
		}

		// streams是一个map,保存所有由 ctx 值键控的活动 grpc 流
		// 如果该请求对应的流为空,则新建
		wgs := w.streams[ctxKey]
		if wgs == nil {
			// newWatcherGrpcStream new一个watch grpc stream来传输watch请求
			// 创建goroutine来处理监听key的watch各种事件
			wgs = w.newWatcherGrpcStream(ctx)
			w.streams[ctxKey] = wgs
		}
		donec := wgs.donec
		reqc := wgs.reqc
		w.mu.Unlock()

		// couldn't create channel; return closed channel
		if closeCh == nil {
			closeCh = make(chan WatchResponse, 1)
		}

		// 等待接收值
		select {
		// reqc 从 Watch() 向主协程发送观察请求
		case reqc <- wr:
			ok = true
		case <-wr.ctx.Done():
			ok = false
		case <-donec:
			ok = false
			if wgs.closeErr != nil {
				closeCh <- WatchResponse{Canceled: true, closeErr: wgs.closeErr}
				break
			}
			// 重试，可能已经从没有 ctxs 中删除了流
			continue
		}

		// receive channel
		if ok {
			select {
			case ret := <-wr.retc:
				return ret
			case <-ctx.Done():
			case <-donec:
				if wgs.closeErr != nil {
					closeCh <- WatchResponse{Canceled: true, closeErr: wgs.closeErr}
					break
				}
				// 重试，可能已经从没有 ctxs 中删除了流
				continue
			}
		}
		break
	}

	close(closeCh)
	return closeCh
}
```

总结：  

1、判断key是否满足watch的条件；  

2、过滤监听事件；  

3、构造watch请求；  

4、查找或分配新的grpc watch stream；  

5、发送watch请求到reqc通道；  

6、返回WatchResponse 接收chan给客户端；  

#### newWatcherGrpcStream

new一个watch grpc stream来传输watch请求

```go
// newWatcherGrpcStream new一个watch grpc stream来传输watch请求
func (w *watcher) newWatcherGrpcStream(inctx context.Context) *watchGrpcStream {
	ctx, cancel := context.WithCancel(&valCtx{inctx})

	//构造watchGrpcStream
	wgs := &watchGrpcStream{
		owner:      w,
		remote:     w.remote,
		callOpts:   w.callOpts,
		ctx:        ctx,
		ctxKey:     streamKeyFromCtx(inctx),
		cancel:     cancel,
		substreams: make(map[int64]*watcherStream),
		respc:      make(chan *pb.WatchResponse),
		reqc:       make(chan watchStreamRequest),
		donec:      make(chan struct{}),
		errc:       make(chan error, 1),
		closingc:   make(chan *watcherStream),
		resumec:    make(chan struct{}),
	}

	// 创建goroutine来处理监听key的watch各种事件
	go wgs.run()
	return wgs
}
```

总结：  

1、构造watchGrpcStream；  

2、创建goroutine也就是run来处理监听key的watch各种事件；  

#### run

处理监听key的watch各种事件

```go
// 通过etcd grpc服务器启动一个watch stream
// run 管理watch 的事件chan
func (w *watchGrpcStream) run() {
	var wc pb.Watch_WatchClient
	var closeErr error

	closing := make(map[*watcherStream]struct{})

	defer func() {
		w.closeErr = closeErr
		// shutdown substreams and resuming substreams
		for _, ws := range w.substreams {
			if _, ok := closing[ws]; !ok {
				close(ws.recvc)
				closing[ws] = struct{}{}
			}
		}
		for _, ws := range w.resuming {
			if _, ok := closing[ws]; ws != nil && !ok {
				close(ws.recvc)
				closing[ws] = struct{}{}
			}
		}
		w.joinSubstreams()
		for range closing {
			w.closeSubstream(<-w.closingc)
		}
		w.wg.Wait()
		w.owner.closeStream(w)
	}()

	// 使用 etcd grpc 服务器启动一个流
	if wc, closeErr = w.newWatchClient(); closeErr != nil {
		return
	}

	cancelSet := make(map[int64]struct{})

	var cur *pb.WatchResponse
	for {
		select {
		// Watch() 请求
		case req := <-w.reqc:
			switch wreq := req.(type) {
			case *watchRequest:
				outc := make(chan WatchResponse, 1)
				// TODO: pass custom watch ID?
				ws := &watcherStream{
					initReq: *wreq,
					id:      -1,
					outc:    outc,
					// unbuffered so resumes won't cause repeat events
					recvc: make(chan *WatchResponse),
				}

				ws.donec = make(chan struct{})
				w.wg.Add(1)
				go w.serveSubstream(ws, w.resumec)

				// queue up for watcher creation/resume
				w.resuming = append(w.resuming, ws)
				if len(w.resuming) == 1 {
					// head of resume queue, can register a new watcher
					if err := wc.Send(ws.initReq.toPB()); err != nil {
						w.lg.Debug("error when sending request", zap.Error(err))
					}
				}
			case *progressRequest:
				if err := wc.Send(wreq.toPB()); err != nil {
					w.lg.Debug("error when sending request", zap.Error(err))
				}
			}

			// 来自watch client的新事件
		case pbresp := <-w.respc:
			if cur == nil || pbresp.Created || pbresp.Canceled {
				cur = pbresp
			} else if cur != nil && cur.WatchId == pbresp.WatchId {
				// merge new events
				// 合并新事件
				cur.Events = append(cur.Events, pbresp.Events...)
				// update "Fragment" field; last response with "Fragment" == false
				cur.Fragment = pbresp.Fragment
			}

			switch {
			// 表示是创建的请求
			case pbresp.Created:
				// response to head of queue creation
				if len(w.resuming) != 0 {
					if ws := w.resuming[0]; ws != nil {
						w.addSubstream(pbresp, ws)
						w.dispatchEvent(pbresp)
						w.resuming[0] = nil
					}
				}

				if ws := w.nextResume(); ws != nil {
					if err := wc.Send(ws.initReq.toPB()); err != nil {
						w.lg.Debug("error when sending request", zap.Error(err))
					}
				}

				// 为下一次迭代重置
				cur = nil
				// 表示取消的请求
			case pbresp.Canceled && pbresp.CompactRevision == 0:
				delete(cancelSet, pbresp.WatchId)
				if ws, ok := w.substreams[pbresp.WatchId]; ok {
					// signal to stream goroutine to update closingc
					close(ws.recvc)
					closing[ws] = struct{}{}
				}

				// reset for next iteration
				cur = nil

				//因为是流的方式传输，所以支持分片传输，遇到分片事件直接跳过
			case cur.Fragment:
				continue

			default:
				// dispatch to appropriate watch stream
				ok := w.dispatchEvent(cur)

				// reset for next iteration
				cur = nil

				if ok {
					break
				}

				// watch response on unexpected watch id; cancel id
				if _, ok := cancelSet[pbresp.WatchId]; ok {
					break
				}

				cancelSet[pbresp.WatchId] = struct{}{}
				cr := &pb.WatchRequest_CancelRequest{
					CancelRequest: &pb.WatchCancelRequest{
						WatchId: pbresp.WatchId,
					},
				}
				req := &pb.WatchRequest{RequestUnion: cr}
				w.lg.Debug("sending watch cancel request for failed dispatch", zap.Int64("watch-id", pbresp.WatchId))
				if err := wc.Send(req); err != nil {
					w.lg.Debug("failed to send watch cancel request", zap.Int64("watch-id", pbresp.WatchId), zap.Error(err))
				}
			}

		// 查看client Recv失败。如果可能，生成另一个，重新尝试发送watch请求
		// 证明发送watch请求失败，会创建watch client再次尝试发送
		case err := <-w.errc:
			if isHaltErr(w.ctx, err) || toErr(w.ctx, err) == v3rpc.ErrNoLeader {
				closeErr = err
				return
			}
			if wc, closeErr = w.newWatchClient(); closeErr != nil {
				return
			}
			if ws := w.nextResume(); ws != nil {
				if err := wc.Send(ws.initReq.toPB()); err != nil {
					w.lg.Debug("error when sending request", zap.Error(err))
				}
			}
			cancelSet = make(map[int64]struct{})

		case <-w.ctx.Done():
			return
			// closurec 获取关闭观察者的观察者流
		case ws := <-w.closingc:
			w.closeSubstream(ws)
			delete(closing, ws)
			// no more watchers on this stream, shutdown, skip cancellation
			if len(w.substreams)+len(w.resuming) == 0 {
				return
			}
			if ws.id != -1 {
				// 客户端正在关闭一个已建立的监视；在服务器上主动关闭它而不是等待
				// 在下一条消息到达时关闭
				cancelSet[ws.id] = struct{}{}
				cr := &pb.WatchRequest_CancelRequest{
					CancelRequest: &pb.WatchCancelRequest{
						WatchId: ws.id,
					},
				}
				req := &pb.WatchRequest{RequestUnion: cr}
				w.lg.Debug("sending watch cancel request for closed watcher", zap.Int64("watch-id", ws.id))
				if err := wc.Send(req); err != nil {
					w.lg.Debug("failed to send watch cancel request", zap.Int64("watch-id", ws.id), zap.Error(err))
				}
			}
		}
	}
}

// dispatchEvent 将 WatchResponse 发送到适当的观察者流
func (w *watchGrpcStream) dispatchEvent(pbresp *pb.WatchResponse) bool {
	events := make([]*Event, len(pbresp.Events))
	for i, ev := range pbresp.Events {
		events[i] = (*Event)(ev)
	}
	// TODO: return watch ID?
	wr := &WatchResponse{
		Header:          *pbresp.Header,
		Events:          events,
		CompactRevision: pbresp.CompactRevision,
		Created:         pbresp.Created,
		Canceled:        pbresp.Canceled,
		cancelReason:    pbresp.CancelReason,
	}

	// 如果watch IDs 索引是0, 所以watch resp 的watch ID 分配为 -1 ，并广播这个watch response
	if wr.IsProgressNotify() && pbresp.WatchId == -1 {
		return w.broadcastResponse(wr)
	}

	return w.unicastResponse(wr, pbresp.WatchId)

}
```

总结：  

1、通过etcd grpc服务器启动一个watch stream；  

2、select检测各个chan的事件（reqc、respc、errc、closingc）；  

3、dispatchEvent 分发事件，处理；  


#### newWatchClient

再来看下`newWatchClient`，创建一个`grpc client`连接`etcd grpc server`   

```go
func (w *watchGrpcStream) newWatchClient() (pb.Watch_WatchClient, error) {
	// 将所有订阅的stream标记为恢复
	close(w.resumec)
	w.resumec = make(chan struct{})
	w.joinSubstreams()
	for _, ws := range w.substreams {
		ws.id = -1
		w.resuming = append(w.resuming, ws)
	}
	// 去掉无用，即为nil的stream
	var resuming []*watcherStream
	for _, ws := range w.resuming {
		if ws != nil {
			resuming = append(resuming, ws)
		}
	}
	w.resuming = resuming
	w.substreams = make(map[int64]*watcherStream)

	// 连接到grpc stream，并且接受watch取消
	stopc := make(chan struct{})
	donec := w.waitCancelSubstreams(stopc)
	wc, err := w.openWatchClient()
	close(stopc)
	<-donec

	// 对于client出错的stream，可以关闭，并且创建一个goroutine，用于转发从run()得到的响应给订阅者
	for _, ws := range w.resuming {
		if ws.closing {
			continue
		}
		ws.donec = make(chan struct{})
		w.wg.Add(1)
		go w.serveSubstream(ws, w.resumec)
	}

	if err != nil {
		return nil, v3rpc.Error(err)
	}

	// 创建goroutine接收来自新grpc流的数据
	go w.serveWatchClient(wc)
	return wc, nil
}

// serveWatchClient 将从grpc stream收到的消息转发到run()
func (w *watchGrpcStream) serveWatchClient(wc pb.Watch_WatchClient) {
	for {
		resp, err := wc.Recv()
		if err != nil {
			select {
			case w.errc <- err:
			case <-w.donec:
			}
			return
		}
		select {
		case w.respc <- resp:
		case <-w.donec:
			return
		}
	}
}
```

总结：  

1、将所有订阅的stream标记为恢复；  

2、连接到grpc stream，并且接受watch取消；  

3、关闭出错的client stream，并且创建goroutine，用于转发从run()得到的响应给订阅者；  

4、创建goroutine接收来自新grpc流的数据。

#### serveSubstream

```go
// serveSubstream 将 watch 响应从 run() 转发给订阅者
func (w *watchGrpcStream) serveSubstream(ws *watcherStream, resumec chan struct{}) {
	if ws.closing {
		panic("created substream goroutine but substream is closing")
	}

	// nextRev is the minimum expected next revision
	nextRev := ws.initReq.rev
	resuming := false
	defer func() {
		if !resuming {
			ws.closing = true
		}
		close(ws.donec)
		if !resuming {
			w.closingc <- ws
		}
		w.wg.Done()
	}()

	emptyWr := &WatchResponse{}
	for {
		curWr := emptyWr
		outc := ws.outc

		if len(ws.buf) > 0 {
			curWr = ws.buf[0]
		} else {
			outc = nil
		}
		select {
		case outc <- *curWr:
			if ws.buf[0].Err() != nil {
				return
			}
			ws.buf[0] = nil
			ws.buf = ws.buf[1:]

			// 一旦观察者建立，retc 就会收到一个 chan WatchResponse
			// 读取recvc里面的值
		case wr, ok := <-ws.recvc:
			if !ok {
				// shutdown from closeSubstream
				return
			}
			// 创建
			if wr.Created {
				if ws.initReq.retc != nil {
					ws.initReq.retc <- ws.outc
					// 防止下一次写入占用缓冲通道中的插槽并发布重复的创建事件
					ws.initReq.retc = nil
					// 仅在请求时发送第一个创建事件
					if ws.initReq.createdNotify {
						ws.outc <- *wr
					}
					// once the watch channel is returned, a current revision
					// watch must resume at the store revision. This is necessary
					// 只要watch channel返回，当前revision的watch一定会在store revision是恢复
					// 对于以下情况按预期工作：
					//	wch := m1.Watch("a")
					//	m2.Put("a", "b")
					//	<-wch
					// 如果修订只绑定在第一个观察到的事件上，
					// 如果在发出 Put 之前 wch 断开连接，则重新连接
					// 提交后，它将错过 Put。
					if ws.initReq.rev == 0 {
						nextRev = wr.Header.Revision
					}
				}
			} else {
				// current progress of watch; <= store revision
				nextRev = wr.Header.Revision
			}

			if len(wr.Events) > 0 {
				nextRev = wr.Events[len(wr.Events)-1].Kv.ModRevision + 1
			}
			ws.initReq.rev = nextRev

			// 上面已经发送了创建的事件，
			// 观察者不应发布重复的事件
			if wr.Created {
				continue
			}

			// TODO pause channel if buffer gets too large
			ws.buf = append(ws.buf, wr)
		case <-w.ctx.Done():
			return
		case <-ws.initReq.ctx.Done():
			return
		case <-resumec:
			resuming = true
			return
		}
	}

	// 如果缺少 id 的事件，则延迟发送取消消息
}
```

总结：  

1、`etcd v3 API`采用了gRPC ，而 gRPC 又利用了`HTTP/2 TCP` 链接多路复用（` multiple stream per tcp connection ）`，这样同一个Client的不同watch可以共享同一个TCP连接。  

2、watch支持指定单个 key，也可以指定一个 key 的前缀；  

3、Watch观察将要发生或者已经发生的事件，输入和输出都是流，输入流用于创建和取消观察，输出流发送事件；  

4、WatcherGrpcStream会启动一个协程专门用于通过 gRPC client stream 接收Server端的 watch response，然后将watch response send 到WatcherGrpcStream的watch response channel。  
 
5、 WatcherGrpcStream 也有一个专门的 协程专门用于从watch response channel 读数据，拿到watch response之后，会根据response里面的watchId 从WatcherGrpcStream的map[watchID] WatcherStream 中拿到对应的WatcherStream，并send到WatcherStream里面的WatchReponse channel。  

6、这里的watchId其实是Server端返回给client端的，当client Send Watch request给Server端时候，response会带上watchId, 这个watchId是与watch key是一一对应关系，然后client会建立WatchId与WatcherStream的映射关系。  

7、WatcherStream是具体的 watch response的处理结构，对于每个watch key，WatcherGrpcStream 也会启动一个专门的协程处理WatcherStream里面的watch response channel。  

### server端的代码实现  

来看下总体的架构  

1、etcd服务端创建newWatchableStore开启group监听；  

2、调用mvcc中syncWatchers将所有未通知的事件通知给所有的监听者；  

3、对watcher通道阻塞时存入victim中数据，开启syncVictimsLoop；  

4、watchServer响应客户端请求，发起watchStream及watcher实例新建，并将其添加至unsynced或synced中；  

5、client端通过grpc proxy向watcherServer发送watcher请求；  

6、grpc proxy提供对同一个key的多次watch合并减少etcd server中重复watcher创建，以提高etcd server稳定性。  


<img src="/img/etcd-server.png" alt="etcd" align=center/>

#### watchableStore

先来看下`watchableStore`   

```go
// 文件 /mvcc/watchable_store.go
type watchableStore struct {
	*store
	mu sync.RWMutex
	// 当ch被阻塞时，对应 watcherBatch 实例会暂时记录到这个字段
	victims []watcherBatch
	// 当有新的 watcherBatch 实例添加到 victims 字段时，会向该通道发送消息
	victimc chan struct{}
	// 未同步的 watcher
	unsynced watcherGroup
	// 已完成同步的 watcher
	synced watcherGroup
	stopc chan struct{}
	wg sync.WaitGroup
}

type watcher struct {
	// 监听起始值
	key []byte
	// 监听终止值，  key 和 end 共同组成一个键值范围
	end []byte
	// 是否被阻塞
	victim bool
	// 是否压缩
	compacted bool
	...
	// 最小的 revision main
	minRev int64
	id     WatchID
	...
	ch chan<- WatchResponse
}

// server/mvcc/watchable_store.go
func newWatchableStore(lg *zap.Logger, b backend.Backend, le lease.Lessor, cfg StoreConfig) *watchableStore {
	if lg == nil {
		lg = zap.NewNop()
	}
	s := &watchableStore{
		store:    NewStore(lg, b, le, cfg),
		victimc:  make(chan struct{}, 1),
		unsynced: newWatcherGroup(),
		synced:   newWatcherGroup(),
		stopc:    make(chan struct{}),
	}
	s.store.ReadView = &readView{s}
	s.store.WriteView = &writeView{s}
	if s.le != nil {
		// use this store as the deleter so revokes trigger watch events
		s.le.SetRangeDeleter(func() lease.TxnDelete { return s.Write(traceutil.TODO()) })
	}
	s.wg.Add(2)
	// 开2个协程
    // syncWatchersLoop 每 100 毫秒同步一次未同步映射中的观察者。
	go s.syncWatchersLoop()
    // syncVictimsLoop 同步预先发送未成功的watchers
	go s.syncVictimsLoop()
	return s
}
```

总结

1、初始化一个watchableStore；  

2、启动了两个协程  

- syncWatchersLoop:每 100 毫秒同步一次未同步映射中的观察者；  

- syncVictimsLoop:同步预先发送未成功的watchers；  

#### syncWatchersLoop

`syncWatchersLoop`会调用`syncWatchers`来进行`watcher`的同步操作    

```go
// syncWatchersLoop 每 100 毫秒同步一次未同步映射中的观察者。
func (s *watchableStore) syncWatchersLoop() {
	defer s.wg.Done()

	for {
		s.mu.RLock()
		st := time.Now()
		lastUnsyncedWatchers := s.unsynced.size()
		s.mu.RUnlock()

		unsyncedWatchers := 0
		//如果 unsynced 中存在数据，进行同步
		if lastUnsyncedWatchers > 0 {
			unsyncedWatchers = s.syncWatchers()
		}
		syncDuration := time.Since(st)

		waitDuration := 100 * time.Millisecond
		// more work pending?
		if unsyncedWatchers != 0 && lastUnsyncedWatchers > unsyncedWatchers {
			// be fair to other store operations by yielding time taken
			waitDuration = syncDuration
		}

		select {
		case <-time.After(waitDuration):
		case <-s.stopc:
			return
		}
	}
}
```

总结：  

1、如果unsynced中存在数据，进行同步；  

2、`100 * time.Millisecond`循环调用一次。  

#### syncWatchers

再来看下`syncWatchers`  

```go
// syncWatchers 通过以下方式同步未同步的观察者：
// 1. 从未同步的观察者组中选择一组观察者
// 2. 迭代集合以获得最小修订并移除压缩的观察者
// 3. 使用最小修订来获取所有键值对并将这些事件发送给观察者
// 4. 从未同步组中移除集合中的同步观察者并移至同步组
func (s *watchableStore) syncWatchers() int {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.unsynced.size() == 0 {
		return 0
	}

	s.store.revMu.RLock()
	defer s.store.revMu.RUnlock()

	// 为了从未同步的观察者中找到键值对，我们需要
	// 找到最小修订索引，这些修订可用于
	// 查询键值对的后端存储
	curRev := s.store.currentRev
	compactionRev := s.store.compactMainRev

	wg, minRev := s.unsynced.choose(maxWatchersPerSync, curRev, compactionRev)
	minBytes, maxBytes := newRevBytes(), newRevBytes()
	revToBytes(revision{main: minRev}, minBytes)
	revToBytes(revision{main: curRev + 1}, maxBytes)

	// UnsafeRange 返回键和值。在 boltdb 中，键是revisions。
	// 值是后端的实际键值对。
	tx := s.store.b.ReadTx()
	tx.RLock()
	revs, vs := tx.UnsafeRange(buckets.Key, minBytes, maxBytes, 0)
	tx.RUnlock()
	evs := kvsToEvents(s.store.lg, wg, revs, vs)

	var victims watcherBatch
	// newWatcherBatch 将观察者映射到它们匹配的事件。也就是一个map中，可以使观察者快速找到匹配的事件
	wb := newWatcherBatch(wg, evs)
	for w := range wg.watchers {
		w.minRev = curRev + 1

		eb, ok := wb[w]
		if !ok {
			// 同步未同步的观察者
			s.synced.add(w)
			s.unsynced.delete(w)
			continue
		}

		if eb.moreRev != 0 {
			w.minRev = eb.moreRev
		}

		// 将前面创建的 Event 事件封装成 WatchResponse,然后写入 watcher.ch 通道中
		if w.send(WatchResponse{WatchID: w.id, Events: eb.evs, Revision: curRev}) {
			pendingEventsGauge.Add(float64(len(eb.evs)))
		} else {
			// 如果阻塞，操作放回到victims中
			if victims == nil {
				victims = make(watcherBatch)
			}
			w.victim = true
		}

		if w.victim {
			victims[w] = eb
		} else {
			// 表示后面还有更多的事件
			if eb.moreRev != 0 {
				// 保持未同步，继续
				continue
			}
			// 标注已经同步
			s.synced.add(w)
		}
		// 从未同步中移除
		s.unsynced.delete(w)
	}
	// 添加阻塞
	s.addVictim(victims)

	vsz := 0
	for _, v := range s.victims {
		vsz += len(v)
	}
	slowWatcherGauge.Set(float64(s.unsynced.size() + vsz))

	return s.unsynced.size()
}
```

总结：  

1、`syncWatchers`中的主要作用是同步未同步的观察者；

2、同时也会将前面创建的Event事件封装成`WatchResponse`,然后写入`watcher.ch`通道中，`sendLoop`监听channel就能，及时通知客户端key的变更。  

#### syncVictimsLoop

再来看下`syncVictimsLoop`  

```go
// syncVictimsLoop tries to write precomputed watcher responses to
// watchers that had a blocked watcher channel
func (s *watchableStore) syncVictimsLoop() {
	defer s.wg.Done()

	for {
		// 将 victims 中的数据尝试发送出去
		for s.moveVictims() != 0 {
			// try to update all victim watchers
		}
		s.mu.RLock()
		isEmpty := len(s.victims) == 0
		s.mu.RUnlock()

		var tickc <-chan time.Time
		if !isEmpty {
			tickc = time.After(10 * time.Millisecond)
		}

		select {
		case <-tickc:
		case <-s.victimc:
		case <-s.stopc:
			return
		}
	}
}
```

主要是调用了moveVictims，接下来看下moveVictims的实现  

#### moveVictims

```go
// moveVictims 尝试watches,如果有pending的event
func (s *watchableStore) moveVictims() (moved int) {
	s.mu.Lock()
	victims := s.victims
	s.victims = nil
	s.mu.Unlock()

	var newVictim watcherBatch
	for _, wb := range victims {
		// 尝试再次发送
		for w, eb := range wb {
			// watcher has observed the store up to, but not including, w.minRev
			rev := w.minRev - 1
			// 将前面创建的 Event 事件封装成 WatchResponse,然后写入 watcher.ch 通道中
			if w.send(WatchResponse{WatchID: w.id, Events: eb.evs, Revision: rev}) {
				pendingEventsGauge.Add(float64(len(eb.evs)))
			} else {
				// 如果阻塞继续放回victims
				if newVictim == nil {
					newVictim = make(watcherBatch)
				}
				newVictim[w] = eb
				continue
			}
			moved++
		}

		// 将victim分配到 unsync/sync中
		s.mu.Lock()
		s.store.revMu.RLock()
		curRev := s.store.currentRev
		for w, eb := range wb {
			if newVictim != nil && newVictim[w] != nil {
				// 无法发送继续放回到victim中
				continue
			}
			w.victim = false
			if eb.moreRev != 0 {
				w.minRev = eb.moreRev
			}
			// currentRev 是最后完成的事务的revision。
			// minRev 是观察者将接受的最小revision的更新
			// 说明这一部分还没有同步到
			if w.minRev <= curRev {
				// 如果未同步，放到unsynced中
				s.unsynced.add(w)
			} else {
				// 同步了直接放入到synced中
				slowWatcherGauge.Dec()
				s.synced.add(w)
			}
		}
		s.store.revMu.RUnlock()
		s.mu.Unlock()
	}

	if len(newVictim) > 0 {
		s.mu.Lock()
		s.victims = append(s.victims, newVictim)
		s.mu.Unlock()
	}

	return moved
}
```

总结：  

1、将 victims 中的数据尝试发送出去；  

2、如果发送仍然阻塞，需要重新放回 victims；  

3、判断这些发送完成的版本号是否小于当前版本号，如果是说明者个过程中有数据更新，还没有同步完成，需要添加到 unsynced 中，等待下次同步。如果不是，说明已经同步完成。  


#### watchServer

```go
// 文件：/etcdserver/api/v3rpc/watch.go
type watchServer struct {
	...
	watchable mvcc.WatchableKV // 键值存储
	....
}

type serverWatchStream struct {
	...
	watchable mvcc.WatchableKV //kv 存储
	...
	// 与客户端进行连接的 Stream
	gRPCStream  pb.Watch_WatchServer
	// key 变动的消息管道
	watchStream mvcc.WatchStream
	// 响应客户端请求的消息管道
	ctrlStream  chan *pb.WatchResponse
	...
	// 该类型的 watch，服务端会定时发送类似心跳消息
	progress map[mvcc.WatchID]bool
	// 该类型表明，对于/a/b 这样的监听范围, 如果 b 变化了， 前缀/a也需要通知
	prevKV map[mvcc.WatchID]bool
	// 该类型表明，传输数据量大于阈值，需要拆分发送
	fragment map[mvcc.WatchID]bool
}

func (ws *watchServer) Watch(stream pb.Watch_WatchServer) (err error) {
	sws := serverWatchStream{
		lg: ws.lg,

		clusterID: ws.clusterID,
		memberID:  ws.memberID,

		maxRequestBytes: ws.maxRequestBytes,

		sg:        ws.sg,
		watchable: ws.watchable,
		ag:        ws.ag,

		gRPCStream:  stream,
		watchStream: ws.watchable.NewWatchStream(),
		// chan for sending control response like watcher created and canceled.
		ctrlStream: make(chan *pb.WatchResponse, ctrlStreamBufLen),

		progress: make(map[mvcc.WatchID]bool),
		prevKV:   make(map[mvcc.WatchID]bool),
		fragment: make(map[mvcc.WatchID]bool),

		closec: make(chan struct{}),
	}

	sws.wg.Add(1)
	go func() {
		// 启动sendLoop
		sws.sendLoop()
		sws.wg.Done()
	}()

	errc := make(chan error, 1)
	// 理想情况下，recvLoop 也会使用 sws.wg 来表示它的完成
	// 但是当 stream.Context().Done() 关闭时，流的 recv
	// 可能会继续阻塞，因为它使用不同的上下文，导致
	// 调用 sws.close() 时死锁。
	go func() {
		// 启动recvLoop
		if rerr := sws.recvLoop(); rerr != nil {
			if isClientCtxErr(stream.Context().Err(), rerr) {
				sws.lg.Debug("failed to receive watch request from gRPC stream", zap.Error(rerr))
			} else {
				sws.lg.Warn("failed to receive watch request from gRPC stream", zap.Error(rerr))
				streamFailures.WithLabelValues("receive", "watch").Inc()
			}
			errc <- rerr
		}
	}()

	// 如果 recv goroutine 在 send goroutine 之前完成，则底层错误（例如 gRPC 流错误）可能会通过 errc 返回和处理。
	// 当 recv goroutine 获胜时，流错误被保留。当 recv 失去竞争时，底层错误就会丢失（除非根错误通过 Context.Err() 传播，但情况并非总是如此（因为调用者必须决定实现自定义上下文才能这样做）
	// stdlib 上下文包内置可能不足以携带语义上有用的错误，应该被重新审视。
	select {
	case err = <-errc:
		if err == context.Canceled {
			err = rpctypes.ErrGRPCWatchCanceled
		}
		close(sws.ctrlStream)
	case <-stream.Context().Done():
		err = stream.Context().Err()
		if err == context.Canceled {
			err = rpctypes.ErrGRPCWatchCanceled
		}
	}

	sws.close()
	return err
}
```

watchServer在上面启动了recvLoop和sendLoop，分别来处理和接收客户度的请求  

#### recvLoop

recvLoop接收客户端请求  

```go
func (sws *serverWatchStream) recvLoop() error {
	for {
		req, err := sws.gRPCStream.Recv()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}

		switch uv := req.RequestUnion.(type) {
		// 处理CreateRequest的请求
		case *pb.WatchRequest_CreateRequest:
			if uv.CreateRequest == nil {
				break
			}

			creq := uv.CreateRequest
			...
			if !sws.isWatchPermitted(creq) {
				// 封装WatchResponse强求
				wr := &pb.WatchResponse{
					Header:       sws.newResponseHeader(sws.watchStream.Rev()),
					WatchId:      creq.WatchId,
					Canceled:     true,
					Created:      true,
					CancelReason: rpctypes.ErrGRPCPermissionDenied.Error(),
				}

				select {
				// ctrlStream响应客户端请求的消息管道
				// 传递WatchResponse请求
				case sws.ctrlStream <- wr:
					continue
				case <-sws.closec:
					return nil
				}
			}

			filters := FiltersFromRequest(creq)

			wsrev := sws.watchStream.Rev()
			rev := creq.StartRevision
			if rev == 0 {
				rev = wsrev + 1
			}
			// Watch 在流中创建一个新的 watcher 并返回它的 WatchID。
			id, err := sws.watchStream.Watch(mvcc.WatchID(creq.WatchId), creq.Key, creq.RangeEnd, rev, filters...)
			...
			wr := &pb.WatchResponse{
				Header:   sws.newResponseHeader(wsrev),
				WatchId:  int64(id),
				Created:  true,
				Canceled: err != nil,
			}
			if err != nil {
				wr.CancelReason = err.Error()
			}
			select {
			// ctrlStream响应客户端请求的消息管道
			// 传递WatchResponse请求
			case sws.ctrlStream <- wr:
			case <-sws.closec:
				return nil
			}
			// 处理CancelRequest的请求
		case *pb.WatchRequest_CancelRequest:
			if uv.CancelRequest != nil {
				id := uv.CancelRequest.WatchId
				err := sws.watchStream.Cancel(mvcc.WatchID(id))
				if err == nil {
					sws.ctrlStream <- &pb.WatchResponse{
						Header:   sws.newResponseHeader(sws.watchStream.Rev()),
						WatchId:  id,
						Canceled: true,
					}
					sws.mu.Lock()
					delete(sws.progress, mvcc.WatchID(id))
					delete(sws.prevKV, mvcc.WatchID(id))
					delete(sws.fragment, mvcc.WatchID(id))
					sws.mu.Unlock()
				}
			}
			// 处理ProgressRequest的请求
		case *pb.WatchRequest_ProgressRequest:
			if uv.ProgressRequest != nil {
				sws.ctrlStream <- &pb.WatchResponse{
					Header:  sws.newResponseHeader(sws.watchStream.Rev()),
					WatchId: -1, // response is not associated with any WatchId and will be broadcast to all watch channels
				}
			}
		default:
			// 我们可能不应该在以下情况下关闭整个流
			// 接收有效命令。
			// 什么都不做。
			continue
		}
	}
}

// server/mvcc/watcher.go
type watchStream struct {
	// 用来记录关联的 watchableStore
	watchable watchable
	// event 事件写入通道
	ch        chan WatchResponse
	...
	cancels  map[WatchID]cancelFunc
	// 用来记录唯一标识与 watcher 的实例的关系
	watchers map[WatchID]*watcher
}

// Watch 在流中创建一个新的 watcher 并返回它的 WatchID。
func (ws *watchStream) Watch(id WatchID, key, end []byte, startRev int64, fcs ...FilterFunc) (WatchID, error) {
	// prevent wrong range where key >= end lexicographically
	// watch request with 'WithFromKey' has empty-byte range end
	if len(end) != 0 && bytes.Compare(key, end) != -1 {
		return -1, ErrEmptyWatcherRange
	}

	ws.mu.Lock()
	defer ws.mu.Unlock()
	if ws.closed {
		return -1, ErrEmptyWatcherRange
	}

	// watch ID在不等于AutoWatchID的时候被使用，否则将会返回一个自增的id
	if id == AutoWatchID {
		for ws.watchers[ws.nextID] != nil {
			ws.nextID++
		}
		id = ws.nextID
		ws.nextID++
	} else if _, ok := ws.watchers[id]; ok {
		return -1, ErrWatcherDuplicateID
	}

	w, c := ws.watchable.watch(key, end, startRev, id, ws.ch, fcs...)

	ws.cancels[id] = c
	ws.watchers[id] = w
	return id, nil
}

func (s *watchableStore) watch(key, end []byte, startRev int64, id WatchID, ch chan<- WatchResponse, fcs ...FilterFunc) (*watcher, cancelFunc) {
	wa := &watcher{
		key:    key,
		end:    end,
		minRev: startRev,
		id:     id,
		ch:     ch,
		fcs:    fcs,
	}
    // 先上一把大的互斥锁
    // 多个watch操作，通过这个互斥锁，保证数据的顺序
	s.mu.Lock()
    // 里面上一把小的读锁
    // 读操作优先，保护读操作
	s.revMu.RLock()
	// 比较 startRev 和 currentRev，决定添加的 watcher 实例是否已经同步
	synced := startRev > s.store.currentRev || startRev == 0
	if synced {
		wa.minRev = s.store.currentRev + 1
		if startRev > wa.minRev {
			wa.minRev = startRev
		}
        // 添加到已同步的 watcher中
		s.synced.add(wa)
	} else {
		slowWatcherGauge.Inc()
        // 添加到未同步的 watcher中
		s.unsynced.add(wa)
	}
	s.revMu.RUnlock()
	s.mu.Unlock()

	watcherGauge.Inc()

	return wa, func() { s.cancelWatcher(wa) }
}
```

总结  

1、接受客户端的请求；  

2、根据不同的请求数据类型进行处理；  

3、主要是通过watchStream来关联watcher，来处理每一个请求。     

#### sendLoop 

响应客户端的请求  

```go
func (sws *serverWatchStream) sendLoop() {
	// watch ids that are currently active
	ids := make(map[mvcc.WatchID]struct{})
	// watch 响应等待 watch id 创建消息
	pending := make(map[mvcc.WatchID][]*pb.WatchResponse)
	...

	for {
		select {
		// 监听key 变动的消息管道
		case wresp, ok := <-sws.watchStream.Chan():
			if !ok {
				return
			}
			...
			canceled := wresp.CompactRevision != 0
			wr := &pb.WatchResponse{
				Header:          sws.newResponseHeader(wresp.Revision),
				WatchId:         int64(wresp.WatchID),
				Events:          events,
				CompactRevision: wresp.CompactRevision,
				Canceled:        canceled,
			}

			if _, okID := ids[wresp.WatchID]; !okID {
				// buffer if id not yet announced
				wrs := append(pending[wresp.WatchID], wr)
				pending[wresp.WatchID] = wrs
				continue
			}

			mvcc.ReportEventReceived(len(evs))

			sws.mu.RLock()
			fragmented, ok := sws.fragment[wresp.WatchID]
			sws.mu.RUnlock()

			var serr error
			if !fragmented && !ok {
				// 通过rpc发送响应给客户端
				serr = sws.gRPCStream.Send(wr)
			} else {
				serr = sendFragments(wr, sws.maxRequestBytes, sws.gRPCStream.Send)
			}

			...
			// 监听响应客户端请求的消息管道
		case c, ok := <-sws.ctrlStream:
			if !ok {
				return
			}

			if err := sws.gRPCStream.Send(c); err != nil {
				if isClientCtxErr(sws.gRPCStream.Context().Err(), err) {
					sws.lg.Debug("failed to send watch control response to gRPC stream", zap.Error(err))
				} else {
					sws.lg.Warn("failed to send watch control response to gRPC stream", zap.Error(err))
					streamFailures.WithLabelValues("send", "watch").Inc()
				}
				return
			}
			....
		case <-progressTicker.C:
			sws.mu.Lock()
			for id, ok := range sws.progress {
				if ok {
					// WatchStream
					// 定时发送 RequestProgress，类似心跳包
					sws.watchStream.RequestProgress(id)
				}
				sws.progress[id] = true
			}
			sws.mu.Unlock()

		case <-sws.closec:
			return
		}
	}
}

type WatchStream interface {
	// Watch 创建了一个观察者. 观察者监听发生在给定的键或范围[key, end]上的事件的变化。
	//
	// 整个事件历史可以被观察，除非压缩。
	// 如果"startRev" <=0, watch观察当前之后的事件。

	// 将返回watcher的id，它显示为WatchID
	// 通过流通道发送给创建的监视器的事件。
	// watch ID在不等于AutoWatchID的时候被使用，否则将会返回一个自增的id
	Watch(id WatchID, key, end []byte, startRev int64, fcs ...FilterFunc) (WatchID, error)

	// Chan返回一个Chan。所有的观察响应将被发送到返回的chan。
	Chan() <-chan WatchResponse

	// RequestProgress请求给定ID的观察者的进度。响应只在观察者当前同步时发送。
	// 响应将通过附加的WatchRespone Chan发送,使用这个流来确保正确的排序。
	// 相应不包含事件。响应中的修订是进度的观察者，因为观察者当前已同步。
	RequestProgress(id WatchID)

	// Cancel 通过给出它的 ID 来取消一个观察者。如果 watcher 不存在，则会报错
	Cancel(id WatchID) error

	// Close closes Chan and release all related resources.
	Close()

	// Rev 返回流监视的 KV 的当前版本。
	Rev() int64
}
```

总结：  

1、通过watchStream.Chan监听key值的变更； 

2、处理 ctrlStream 的消息(客户端请求，返回响应)；  

3、定时发送 RequestProgress 类似心跳包。  

### 连接复用

上面我们提到了连接复用，我们来看看如何实现复用的  

```go
// 其中Watch()函数发送watch请求，第一次发送后递归调用Watch实现持续监听
func (w *watcher) Watch(ctx context.Context, key string, opts ...OpOption) WatchChan {
	ow := opWatch(key, opts...)

	var filters []pb.WatchCreateRequest_FilterType
	if ow.filterPut {
		filters = append(filters, pb.WatchCreateRequest_NOPUT)
	}
	if ow.filterDelete {
		filters = append(filters, pb.WatchCreateRequest_NODELETE)
	}

	wr := &watchRequest{
		ctx:            ctx,
		createdNotify:  ow.createdNotify,
		key:            string(ow.key),
		end:            string(ow.end),
		rev:            ow.rev,
		progressNotify: ow.progressNotify,
		fragment:       ow.fragment,
		filters:        filters,
		prevKV:         ow.prevKV,
		retc:           make(chan chan WatchResponse, 1),
	}

	ok := false
	ctxKey := streamKeyFromCtx(ctx)

	var closeCh chan WatchResponse
	for {
		// 查找或分配适当的 grpc 监视流
		w.mu.Lock()
		if w.streams == nil {
			// closed
			w.mu.Unlock()
			ch := make(chan WatchResponse)
			close(ch)
			return ch
		}

		// streams是一个map,保存所有由 ctx 值键控的活动 grpc 流
		// 如果该请求对应的流为空,则新建
		wgs := w.streams[ctxKey]
		if wgs == nil {
			// newWatcherGrpcStream new一个watch grpc stream来传输watch请求
			// 创建goroutine来处理监听key的watch各种事件
			wgs = w.newWatcherGrpcStream(ctx)
			w.streams[ctxKey] = wgs
		}
		donec := wgs.donec
		reqc := wgs.reqc
		w.mu.Unlock()

		// couldn't create channel; return closed channel
		if closeCh == nil {
			closeCh = make(chan WatchResponse, 1)
		}

		// 等待接收值
		select {
		// reqc 从 Watch() 向主协程发送观察请求
		case reqc <- wr:
			ok = true
		case <-wr.ctx.Done():
			ok = false
		case <-donec:
			ok = false
			if wgs.closeErr != nil {
				closeCh <- WatchResponse{Canceled: true, closeErr: wgs.closeErr}
				break
			}
			// 重试，可能已经从没有 ctxs 中删除了流
			continue
		}

		// receive channel
		if ok {
			select {
			case ret := <-wr.retc:
				return ret
			case <-ctx.Done():
			case <-donec:
				if wgs.closeErr != nil {
					closeCh <- WatchResponse{Canceled: true, closeErr: wgs.closeErr}
					break
				}
				// 重试，可能已经从没有 ctxs 中删除了流
				continue
			}
		}
		break
	}

	close(closeCh)
	return closeCh
}
```

例如这个client的watch  

1、`newWatcherGrpcStream new`一个watchGrpcStream来传输watch请求；  

2、监听watchGrpcStream的reqc.c,来发送请求； 

这俩实现了连接复用，只要没有关闭，就能一直监听发送请求信息。  

### 总结

上面主要总结了etcd中watch机制，client端比较简答，server端的实现比较复杂；   

client主要是提供操作来请求对key监听，并且接收key变更时的通知。server要能做到接收key监听请求，并且启动定时器等方法来对key进行监听，有变更时通知client。  

v3版本中watch依赖gRPC接口，实现连接复用。  






