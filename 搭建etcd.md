## etcd的搭建

### 前言  

这里记录下如何搭建etcd   

### 单机

在etcd的releases中有安装脚本,[](https://github.com/etcd-io/etcd/releases)  

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
| ------ | ------          | 
| etcd-1 | 192.168.56.111 |
| etcd-2 | 192.168.56.112 |
| etcd-3 | 192.168.56.113 |

首先在每台机器中安装etcd,这里写了安装的脚本  

```shell script
# cat etcd.sh 
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

在每台机器中都执行下    

```
# ./etcd.sh
  % Total    % Received % Xferd  Average Speed   Time    Time     Time  Current
                                 Dload  Upload   Total   Spent    Left  Speed
100   636  100   636    0     0   1328      0 --:--:-- --:--:-- --:--:--  1330
100 18.4M  100 18.4M    0     0   717k      0  0:00:26  0:00:26 --:--:--  775k
...
```

创建etcd配置文件   

`$ vi /etc/etcd/conf.yml`   

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

更新etcd系统默认配置  

当前使用的是etcd v3版本，系统默认的是v2，通过下面命令修改配置。  

`
$ vi /etc/profile
# 在末尾追加  
export ETCDCTL_API=3
# 然后更新
$ source /etc/profile
`

启动命令  




### 参考  

【ETCD集群安装配置】https://zhuanlan.zhihu.com/p/46477992    






