# AWS分布式部署

### 节点服务器环境配置

```shell
#推荐使用Ubuntu22.04 LTS
/home/ubuntu目录上传env-batch.sh
sudo apt update
sudo apt install dos2unix
dos2unix env-batch.sh
chmod 777 env-batch.sh
./env-batch.sh
#测试Areopagus运行情况（go编译）
#关机存映像，用该映像重启服务器，存模版
```



## 运行流程

### 环境配置

**本地windows**：装有`aws-cli`，完成`aws configure`配置，python有`boto3`；

**远程Areopagus中控**：环境配置同节点服务器，额外在根目录放置私钥（权限400）



### 具体操作

#### （1）启动各个区域的AWS服务器

推荐在各个区域创建好支持Areopagus运行环境的服务器模版，直接从模版创建实例。

#### （2）部署配置文件

* 在`config.py`顶部设置参数：

| 常量名称                        | 参数含义 |
| -------------------------------- | -------- |
| N_M                              | 主链（`shard0`）中的节点数量 |
| N_W                              | 每个 work shard 中的节点数量 |
| F_M                              | 主链（`shard0`）中的恶意节点数量 |
| F_W                              | 每个 work shard 中的恶意节点数量 |
| SHARD_COUNT                      | 分片个数 |
| WORK_SHARD_NODES_PER_SERVER      | work shard 每台服务器部署的节点数 |
| START_PORT                       | 起始端口 |
| REGION_NAMES                     | 拉取 AWS 实例时扫描的 region 列表 |

* 本地运行`config.py`，脚本会：
  * 按 CPU 核数从高到低挑出 `N_M` 台服务器分给 `shard0`，且 `shard0` 的每个节点独占一台机器；
  * 将剩余服务器按 `region` 排序，再顺序分给各个 work shard，以尽可能让一个 shard 落在同一 region；
  * 打印机器分配摘要，供部署前人工确认；
  * 输出 YAML 配置和 Bash 头部变量块（包含 `N_M`、`N_W`、`F_M`、`F_W`、`nodeCountsVar`、`pubIPsVar`）。

* 将生成的 YAML 配置替换到 `config_local.yaml`；将 Bash 头部变量块替换到 `aws-pre.sh`、`aws-run.sh`、`aws-pull.sh`、`aws-log.sh` 和 `aws-kill.sh` 顶部对应位置。

* 将这些脚本上传到**Areopagus中控的/home/ubuntu目录下**；将`config_local.yaml`上传到**Areopagus中控的/home/ubuntu/Areopagus/cmd/main目录下**。

* 在**Areopagus中控的/home/ubuntu/Areopagus目录下**运行

  ```shell
  #刚需
  go run ./cmd/configMaker/configMaker.go -config_path ./cmd/main/config_local.yaml
  ```

* 在**Areopagus中控的/home/ubuntu目录下**运行	

  ```shell
  dos2unix aws-log.sh aws-pre.sh aws-run.sh aws-pull.sh aws-kill.sh
  ./aws-pre.sh
  ```

​	向各个节点服务器的**Areopagus/configs/\* **传入**一致的**配置文件。

#### （3）运行与获取日志数据

##### kronos

* 编辑`aws-run.sh`：

  ```shell
   nohup ./start_all.sh ${start_node} ${end_node} 0 \"2025-03-30 03:08:00.000\" > server-$i.out
  ```

​	只需要调整这句命令里的0/1(分别对应有无debug日志)和起始运行时间即可。

* 在**Areopagus中控的/home/ubuntu目录下**运行`./aws-run.sh`，完成之后运行`./aws-log.sh`

* 在**Areopagus中控的/home/ubuntu/Areopagus目录下**运行

  ```shell
  go run ./cmd/performance/performanceCal.go
  ```

​	获取正常执行流程中的TPS和时延数据。

