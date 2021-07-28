## etcd的搭建

### 前言  

这里记录下如何搭建etcd   

### 单机

使用docker部署  

```
docker run -d -it --rm \
--name etcd_test \
-e ETCDCTL_API=3 \
-p 2379:2379 \
-p 2380:2380 \
quay.io/coreos/etcd:v3.3.9 \
etcd \
--advertise-client-urls http://0.0.0.0:2379 \
--listen-client-urls http://0.0.0.0:2379  
```



### 集群

