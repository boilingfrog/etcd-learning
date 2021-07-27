## etcd的搭建

### 前言  

这里记录下如何搭建etcd   

### 单机

使用docker部署  

```yaml
version: "3"
 
services:
  etcd:
    image: quay.io/coreos/etcd:v3.4.14
    container_name: etcd
    command: /usr/local/bin/etcd
    restart: always
    networks:
      - etcd-test
    ports:
      - "2379:2379"
      - "2380:2380"
    volumes:
      - "./etcd-data:/etcd-data"
    environment:
      # 指定版本
      ETCDCTL_API: 3
      # 日志类型
      ETCD_LOGGER: capnslog
      # 存储路径
      ETCD_DATA_DIR: /etcd-data
  
      # 节点名称
      ETCD_NAME: node1
      # 创建集群唯一TOKEN
      INITIAL_CLUSTER_TOKEN: etcd-test-cluster
  
      # 该节点同伴监听地址
      ETCD_INITIAL_ADVERTISE_PEER_URLS: http://192.168.56.111:2380
      # 和其他节点通信的地址
      ETCD_LISTEN_PEER_URLS: http://0.0.0.0:2380
  
      # 对外公告该节点客户端监听地址
      ETCD_ADVERTISE_CLIENT_URLS: http://192.168.56.111:2379
      # 对外提供服务的地址
      ETCD_LISTEN_CLIENT_URLS: http://0.0.0.0:2379
 
      # 初始化集群所有节点列表（逗号隔开）
      ETCD_INITIAL_CLUSTER: node1=http://192.168.56.111:2379
      # 集群初始化状态（新建集群时为new）
      ETCD_INITIAL_CLUSTER_STATE: new
      # 启用K-V键值自动压缩存盘
      ETCD_AUTO_COMPACTION_RETENTION: 1
 
 
networks:
  etcd-test:
    external: true
```

### 集群

