# Changelog

## 1.0.3 (2016-12-16)

- IP资源池检查脚本 ip-pool-usage
- 新的etcd数据模型，增加已分配ip的hostname记录
- 增加etcd锁机制以保证并发申请不会获得重复的IP
- 增加初始化过程中清理本机未释放IP功能
- 一键build rpm功能

## 1.0.4 (2016-12-29)

- 创建部署文档

## 1.0.6 (2017-1-25)

- 启动过程使用go routine清理未释放IP

## 2.0.1 (2017-6-5)

- 通过etcd watcher与tc 支持容器限流


