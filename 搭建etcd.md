<!-- START doctoc generated TOC please keep comment here to allow auto update -->
<!-- DON'T EDIT THIS SECTION, INSTEAD RE-RUN doctoc TO UPDATE -->

- [etcd的搭建](#etcd%E7%9A%84%E6%90%AD%E5%BB%BA)
  - [前言](#%E5%89%8D%E8%A8%80)
  - [单机](#%E5%8D%95%E6%9C%BA)
  - [集群](#%E9%9B%86%E7%BE%A4)
    - [创建etcd配置文件](#%E5%88%9B%E5%BB%BAetcd%E9%85%8D%E7%BD%AE%E6%96%87%E4%BB%B6)
    - [更新etcd系统默认配置](#%E6%9B%B4%E6%96%B0etcd%E7%B3%BB%E7%BB%9F%E9%BB%98%E8%AE%A4%E9%85%8D%E7%BD%AE)
    - [启动](#%E5%90%AF%E5%8A%A8)
    - [配置ETCD为启动服务](#%E9%85%8D%E7%BD%AEetcd%E4%B8%BA%E5%90%AF%E5%8A%A8%E6%9C%8D%E5%8A%A1)
  - [测试下](#%E6%B5%8B%E8%AF%95%E4%B8%8B)
  - [参考](#%E5%8F%82%E8%80%83)

<!-- END doctoc generated TOC please keep comment here to allow auto update -->

## etcd的搭建

### 前言  

这里记录下如何搭建etcd   

### 单机

在etcd的releases中有安装脚本,[安装脚本](https://github.com/etcd-io/etcd/releases)  

这里放一个docker的安装脚本  

```shell script
rm -rf /tmp/etcd-data.tmp && mkdir -p /tmp/etcd-data.tmp && \
  docker rmi quay.io/coreos/etcd:v3.5.0 || true && \
  docker run \
  -p 2379:2379 \
  -p 2380:2380 \
  --mount type=bind,source=/tmp/etcd-data.tmp,destination=/etcd-data \
  --name etcd-gcr-v3.5.0 \
  quay.io/coreos/etcd:v3.5.0 \
  /usr/local/bin/etcd \
  --name s1 \
  --data-dir /etcd-data \
  --listen-client-urls http://0.0.0.0:2379 \
  --advertise-client-urls http://0.0.0.0:2379 \
  --listen-peer-urls http://0.0.0.0:2380 \
  --initial-advertise-peer-urls http://0.0.0.0:2380 \
  --initial-cluster s1=http://0.0.0.0:2380 \
  --initial-cluster-token tkn \
  --initial-cluster-state new \
  --log-level info \
  --logger zap \
  --log-outputs stderr
```

### 集群

这里准备了三台`centos7`机器    

| 主机    | ip             |
| ------ | ------         | 
| etcd-1 | 192.168.56.111 |
| etcd-2 | 192.168.56.112 |
| etcd-3 | 192.168.56.113 |

首先在每台机器中安装etcd,这里写了安装的脚本  

```shell script
$ cat etcd.sh 

ETCD_VER=v3.5.0

# choose either URL
GOOGLE_URL=https://storage.googleapis.com/etcd
GITHUB_URL=https://github.com/etcd-io/etcd/releases/download
DOWNLOAD_URL=${GITHUB_URL}

rm -f /tmp/etcd-${ETCD_VER}-linux-amd64.tar.gz
rm -rf /opt/etcd && mkdir -p /opt/etcd

curl -L ${DOWNLOAD_URL}/${ETCD_VER}/etcd-${ETCD_VER}-linux-amd64.tar.gz -o /tmp/etcd-${ETCD_VER}-linux-amd64.tar.gz
tar xzvf /tmp/etcd-${ETCD_VER}-linux-amd64.tar.gz -C /opt/etcd --strip-components=1
rm -f /tmp/etcd-${ETCD_VER}-linux-amd64.tar.gz
```

赋予执行权限
```
$ chmod +x etcd.sh
```

在每台机器中都执行下    

```
$ ./etcd.sh
  % Total    % Received % Xferd  Average Speed   Time    Time     Time  Current
                                 Dload  Upload   Total   Spent    Left  Speed
100   636  100   636    0     0   1328      0 --:--:-- --:--:-- --:--:--  1330
100 18.4M  100 18.4M    0     0   717k      0  0:00:26  0:00:26 --:--:--  775k
...
```

#### 创建etcd配置文件   

```
$ mkdir /etc/etcd
$ vi /etc/etcd/conf.yml
```

节点1  

```
name: etcd-1
data-dir: /opt/etcd/data
listen-client-urls: http://192.168.56.111:2379,http://127.0.0.1:2379
advertise-client-urls: http://192.168.56.111:2379,http://127.0.0.1:2379
listen-peer-urls: http://192.168.56.111:2380
initial-advertise-peer-urls: http://192.168.56.111:2380
initial-cluster: etcd-1=http://192.168.56.111:2380,etcd-2=http://192.168.56.112:2380,etcd-3=http://192.168.56.113:2380
initial-cluster-token: etcd-cluster-token
initial-cluster-state: new
```

节点2  

```
name: etcd-2
data-dir: /opt/etcd/data
listen-client-urls: http://192.168.56.112:2379,http://127.0.0.1:2379
advertise-client-urls: http://192.168.56.112:2379,http://127.0.0.1:2379
listen-peer-urls: http://192.168.56.112:2380
initial-advertise-peer-urls: http://192.168.56.112:2380
initial-cluster: etcd-1=http://192.168.56.111:2380,etcd-2=http://192.168.56.112:2380,etcd-3=http://192.168.56.113:2380
initial-cluster-token: etcd-cluster-token
initial-cluster-state: new
```

节点3  

```
name: etcd-3
data-dir: /opt/etcd/data
listen-client-urls: http://192.168.56.113:2379,http://127.0.0.1:2379
advertise-client-urls: http://192.168.56.113:2379,http://127.0.0.1:2379
listen-peer-urls: http://192.168.56.113:2380
initial-advertise-peer-urls: http://192.168.56.113:2380
initial-cluster: etcd-1=http://192.168.56.111:2380,etcd-2=http://192.168.56.112:2380,etcd-3=http://192.168.56.113:2380
initial-cluster-token: etcd-cluster-token
initial-cluster-state: new
```

#### 更新etcd系统默认配置  

当前使用的是etcd v3版本，系统默认的是v2，通过下面命令修改配置。  

```
$ vi /etc/profile
# 在末尾追加  
export ETCDCTL_API=3
# 然后更新
$ source /etc/profile
```

#### 启动

```
$ ./etcd --config-file=/etc/etcd/conf.yml
```

#### 配置ETCD为启动服务

编辑/usr/lib/systemd/system/etcd.service  

```
$ cat /usr/lib/systemd/system/etcd.service
[Unit]
Description=EtcdServer
After=network.target
After=network-online.target
Wants=network-online.target

[Service]
Type=notify
WorkingDirectory=/opt/etcd/
# User=etcd
ExecStart=/opt/etcd/etcd --config-file=/etc/etcd/conf.yml
Restart=on-failure
LimitNOFILE=65536

[Install]
WantedBy=multi-user.target
```

更新启动：  

```
$ systemctl daemon-reload
$ systemctl enable etcd
$ systemctl start etcd
$ systemctl restart etcd

$ systemctl status etcd.service -l
```

### 测试下  
 
复制etcd二进制文件到`/usr/local/bin/`  

```
$ cp /opt/etcd/etcd* /usr/local/bin/
```

首先设置ETCD_ENDPOINTS  

```
# export ETCDCTL_API=3
# export ETCD_ENDPOINTS=192.168.56.111:2379,192.168.56.112:2379,192.168.56.113:2379
```

查看状态  

```
$ etcdctl --endpoints=${ETCD_ENDPOINTS} --write-out=table member list
+------------------+---------+--------+----------------------------+--------------------------------------------------+------------+
|        ID        | STATUS  |  NAME  |         PEER ADDRS         |                   CLIENT ADDRS                   | IS LEARNER |
+------------------+---------+--------+----------------------------+--------------------------------------------------+------------+
|  90d224ceb3098d7 | started | etcd-2 | http://192.168.56.112:2380 | http://127.0.0.1:2379,http://192.168.56.112:2379 |      false |
| 3b23fbb7d9c7cd10 | started | etcd-1 | http://192.168.56.111:2380 | http://127.0.0.1:2379,http://192.168.56.111:2379 |      false |
| 7909c74e3f5ffafa | started | etcd-3 | http://192.168.56.113:2380 | http://127.0.0.1:2379,http://192.168.56.113:2379 |      false |
+------------------+---------+--------+----------------------------+--------------------------------------------------+------------+

$ etcdctl --endpoints=${ETCD_ENDPOINTS} --write-out=table endpoint health
+---------------------+--------+------------+-------+
|      ENDPOINT       | HEALTH |    TOOK    | ERROR |
+---------------------+--------+------------+-------+
| 192.168.56.111:2379 |   true | 6.558088ms |       |
| 192.168.56.113:2379 |   true | 6.543104ms |       |
| 192.168.56.112:2379 |   true | 7.405801ms |       |
+---------------------+--------+------------+-------+

$ etcdctl --endpoints=${ETCD_ENDPOINTS} --write-out=table endpoint status
+---------------------+------------------+---------+---------+-----------+------------+-----------+------------+--------------------+--------+
|      ENDPOINT       |        ID        | VERSION | DB SIZE | IS LEADER | IS LEARNER | RAFT TERM | RAFT INDEX | RAFT APPLIED INDEX | ERRORS |
+---------------------+------------------+---------+---------+-----------+------------+-----------+------------+--------------------+--------+
| 192.168.56.111:2379 | 3b23fbb7d9c7cd10 |   3.5.0 |   20 kB |      true |      false |         2 |         19 |                 19 |        |
| 192.168.56.112:2379 |  90d224ceb3098d7 |   3.5.0 |   20 kB |     false |      false |         2 |         19 |                 19 |        |
| 192.168.56.113:2379 | 7909c74e3f5ffafa |   3.5.0 |   20 kB |     false |      false |         2 |         19 |                 19 |        |
+---------------------+------------------+---------+---------+-----------+------------+-----------+------------+--------------------+--------+
```

在etcd-1中watch一个key,然后再etcd-2中对key设置一个值  

```
[root@centos7-1 ~]# etcdctl watch test
PUT
test
xiaoming

[root@centos7-3 ~]# etcdctl put test xiaoming
OK
```

### 参考  

【ETCD集群安装配置】https://zhuanlan.zhihu.com/p/46477992    






